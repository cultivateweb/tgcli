package daemon

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/cultivateweb/tgcli/internal/config"
	"github.com/cultivateweb/tgcli/internal/ipc"
)

// newTestDaemon поднимает серверную часть демона (без Telegram) на временном
// сокете и возвращает демон, путь сокета и его главный контекст (его отменяет
// команда stop через d.cancel).
func newTestDaemon(t *testing.T) (*Daemon, string, context.Context) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	d := &Daemon{
		startedAt: time.Now(),
		self:      "Тест",
		cancel:    cancel,
		subs:      make(map[chan ipc.MessageEvent]struct{}),
	}
	go d.acceptLoop(ctx, ln)
	return d, sock, ctx
}

// waitSubs ждёт, пока число подписчиков достигнет want (защита от гонки между
// подключением клиента и регистрацией подписки на сервере).
func waitSubs(t *testing.T, d *Daemon, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.statusReply().Subscribers == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("подписчиков не стало %d (сейчас %d)", want, d.statusReply().Subscribers)
}

func TestStatusReply(t *testing.T) {
	_, sock, _ := newTestDaemon(t) //nolint:dogsled

	st, err := ipc.Status(sock)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Self != "Тест" {
		t.Errorf("self = %q, ожидалось «Тест»", st.Self)
	}
	if st.Subscribers != 0 {
		t.Errorf("подписчиков = %d, ожидалось 0", st.Subscribers)
	}
}

func TestEventsBroadcast(t *testing.T) {
	d, sock, _ := newTestDaemon(t)

	conn, dec, err := ipc.Open(sock, ipc.CmdEvents)
	if err != nil {
		t.Fatalf("open events: %v", err)
	}
	defer conn.Close()

	waitSubs(t, d, 1)
	d.broadcast(ipc.MessageEvent{From: "Аня", Text: "привет"})

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env ipc.Envelope
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Type != ipc.TypeMessage || env.Message == nil {
		t.Fatalf("тип кадра = %q, message=%v", env.Type, env.Message)
	}
	if env.Message.From != "Аня" || env.Message.Text != "привет" {
		t.Errorf("событие = %+v", env.Message)
	}
}

func TestStopCancels(t *testing.T) {
	_, sock, ctx := newTestDaemon(t)

	if err := ipc.Stop(sock); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Команда stop должна отменить главный контекст демона.
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("контекст демона не отменился после stop")
	}
}

func TestAcquireLockRejectsSecond(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d := New(cfg)
	if err := d.acquireLock(); err != nil {
		t.Fatalf("первый lock: %v", err)
	}
	defer d.releaseLock()

	// Второй экземпляр видит живой pid (наш процесс) и отказывается стартовать.
	d2 := New(cfg)
	if err := d2.acquireLock(); err == nil {
		t.Error("второй lock прошёл, ожидалась ошибка «уже запущен»")
	}
}

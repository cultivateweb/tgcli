// Package daemon реализует фоновый резидент tgcli: один процесс держит живую
// MTProto-сессию, ловит входящие сообщения и раздаёт их тонким клиентам через
// unix-сокет (команда events), а также шлёт desktop-уведомления.
//
// Резидент намеренно не демонизируется сам (double-fork): он работает на
// переднем плане и блокируется до отмены контекста (Ctrl+C / SIGTERM) или
// команды stop. Фоновость обеспечивает окружение — «tgcli daemon &», systemd
// user-unit и т.п. Это проще и надёжнее ручного отрыва от терминала.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cultivateweb/tgcli/internal/config"
	"github.com/cultivateweb/tgcli/internal/ipc"
	"github.com/cultivateweb/tgcli/internal/telegram"
)

// Daemon — резидент. Держит соединение с Telegram и список подписчиков на
// поток событий.
type Daemon struct {
	cfg    *config.Config
	client *telegram.Client

	startedAt time.Time
	self      string             // под кем залогинен (для status)
	cancel    context.CancelFunc // отмена главного контекста (команда stop)

	mu   sync.Mutex
	subs map[chan ipc.MessageEvent]struct{}
}

// New создаёт резидент на основе конфигурации.
func New(cfg *config.Config) *Daemon {
	return &Daemon{
		cfg:       cfg,
		client:    telegram.New(cfg),
		startedAt: time.Now(),
		subs:      make(map[chan ipc.MessageEvent]struct{}),
	}
}

// Run запускает резидент и блокируется до отмены ctx или команды stop.
func (d *Daemon) Run(ctx context.Context) error {
	// Защита от второго экземпляра.
	if err := d.acquireLock(); err != nil {
		return err
	}
	defer d.releaseLock()

	// Ранняя проверка авторизации и имя аккаунта для status. Заодно отсекаем
	// запуск без входа понятной ошибкой ещё до подъёма сокета.
	authorized, self, err := d.client.Status(ctx)
	if err != nil {
		return err
	}
	if !authorized {
		return telegram.ErrNotAuthorized
	}
	d.self = telegram.DisplayName(self)

	// Слушаем unix-сокет. Несвежий файл сокета (после грязного выхода) убираем —
	// lock выше уже подтвердил, что другого живого демона нет.
	socketPath := d.cfg.SocketPath()
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("прослушивание сокета %s: %w", socketPath, err)
	}
	_ = os.Chmod(socketPath, 0o600)
	defer func() {
		ln.Close()
		os.Remove(socketPath)
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.cancel = cancel

	// Закрываем listener при отмене контекста, чтобы разблокировать Accept.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	go d.acceptLoop(ctx, ln)

	fmt.Fprintf(os.Stderr, "tgcli daemon: онлайн как %s, сокет %s\n", d.self, socketPath)

	// Живая сессия: крутим раздачу входящих, пока контекст жив.
	err = d.client.WithLiveSession(ctx, func(ctx context.Context, _ *telegram.Session, updates <-chan telegram.NewMessage) error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case nm, ok := <-updates:
				if !ok {
					return nil
				}
				d.dispatch(nm)
			}
		}
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// dispatch раздаёт входящее сообщение подписчикам и шлёт desktop-уведомление
// (на чужие сообщения, не на собственные исходящие).
func (d *Daemon) dispatch(nm telegram.NewMessage) {
	ev := ipc.MessageEvent{
		PeerKey: nm.PeerKey,
		From:    nm.Message.Author,
		Text:    nm.Message.Plain(),
		Out:     nm.Message.Out,
		Time:    nm.Message.Date,
	}
	if nm.Message.Media != nil {
		ev.Media = nm.Message.Media.Label()
	}
	d.broadcast(ev)
	if !ev.Out {
		notify(ev)
	}
}

// broadcast неблокирующе рассылает событие всем подписчикам: медленный клиент
// пропустит сообщение, но не задержит остальных и обработчик обновлений.
func (d *Daemon) broadcast(ev ipc.MessageEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (d *Daemon) subscribe() chan ipc.MessageEvent {
	ch := make(chan ipc.MessageEvent, 64)
	d.mu.Lock()
	d.subs[ch] = struct{}{}
	d.mu.Unlock()
	return ch
}

func (d *Daemon) unsubscribe(ch chan ipc.MessageEvent) {
	d.mu.Lock()
	delete(d.subs, ch)
	d.mu.Unlock()
}

func (d *Daemon) statusReply() *ipc.StatusReply {
	d.mu.Lock()
	n := len(d.subs)
	d.mu.Unlock()
	return &ipc.StatusReply{Self: d.self, StartedAt: d.startedAt, Subscribers: n}
}

// acceptLoop принимает соединения, пока listener жив. Каждое обслуживается в
// своей горутине.
func (d *Daemon) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return // штатное закрытие listener
			}
			continue
		}
		go d.handleConn(ctx, conn)
	}
}

// handleConn читает одну команду и обслуживает её.
func (d *Daemon) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	var req ipc.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	enc := json.NewEncoder(conn)

	switch req.Cmd {
	case ipc.CmdStatus:
		_ = enc.Encode(ipc.Envelope{Type: ipc.TypeStatus, Status: d.statusReply()})
	case ipc.CmdStop:
		_ = enc.Encode(ipc.Envelope{Type: ipc.TypeOK})
		if d.cancel != nil {
			d.cancel()
		}
	case ipc.CmdEvents:
		d.streamEvents(ctx, conn, enc)
	default:
		_ = enc.Encode(ipc.Envelope{Type: ipc.TypeError, Error: "неизвестная команда: " + req.Cmd})
	}
}

// streamEvents отдаёт клиенту поток входящих, пока жив контекст и соединение.
func (d *Daemon) streamEvents(ctx context.Context, conn net.Conn, enc *json.Encoder) {
	ch := d.subscribe()
	defer d.unsubscribe(ch)

	// Замечаем уход клиента: чтение из conn вернёт ошибку, когда он закроет
	// соединение (events-клиент сам ничего не шлёт после команды).
	gone := make(chan struct{})
	go func() {
		var buf [1]byte
		_, _ = conn.Read(buf[:])
		close(gone)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-gone:
			return
		case ev := <-ch:
			if err := enc.Encode(ipc.Envelope{Type: ipc.TypeMessage, Message: &ev}); err != nil {
				return
			}
		}
	}
}

// acquireLock записывает pid в pid-файл, если другой живой демон не держит его.
func (d *Daemon) acquireLock() error {
	pidPath := d.cfg.PidPath()
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr == nil && pid > 0 {
			if processAlive(pid) {
				return fmt.Errorf("демон уже запущен (pid %d); остановите его «tgcli daemon --stop»", pid)
			}
		}
		// Несвежий pid (процесс мёртв) — затираем.
	}
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func (d *Daemon) releaseLock() {
	os.Remove(d.cfg.PidPath())
}

// processAlive проверяет, жив ли процесс с данным pid (сигнал 0 — проба).
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

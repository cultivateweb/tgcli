package tui

import (
	"runtime"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

// newTestUI поднимает интерфейс в реальном цикле app.Run на симуляционном
// экране. Возвращает ui, экран и функцию проверки «жив ли событийный цикл».
func newTestUI(t *testing.T) (*ui, tcell.SimulationScreen, func(scenario string)) {
	t.Helper()
	u := &ui{app: tview.NewApplication(), selected: map[int]bool{}, menuActive: -1, version: "test"}
	u.build()
	u.pages = tview.NewPages().AddPage("main", u.root(), true, true)

	// Наполняем реальными данными: дерево чатов, открытый чат, история.
	u.showDetails = true
	u.rebuildMid()
	u.setDialogs([]telegram.Dialog{
		{Title: "Alice", Kind: "user", CanSend: true, Ref: telegram.PeerRef{Type: "user", ID: 1}},
		{Title: "Bob", Kind: "user", CanSend: true, Ref: telegram.PeerRef{Type: "user", ID: 2}},
	})
	u.open = &telegram.Dialog{Title: "Alice", Kind: "user", CanSend: true, Ref: telegram.PeerRef{Type: "user", ID: 1}}
	u.msgTitle = " Alice "
	u.history = []telegram.HistoryMessage{
		{ID: 1, Author: "Alice", Text: "привет"},
		{ID: 2, Out: true, Text: "здорово"},
	}
	u.renderMessages()
	u.renderDetails()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(120, 40)
	u.app.SetScreen(screen)
	u.app.SetRoot(u.pages, true).EnableMouse(true)
	go func() {
		u.app.SetFocus(u.tree)
		_ = u.app.Run()
	}()
	time.Sleep(200 * time.Millisecond)

	checkAlive := func(scenario string) {
		done := make(chan struct{})
		go u.app.QueueUpdateDraw(func() { close(done) })
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Fatalf("ДЕДЛОК в сценарии %q.\nСтеки горутин:\n%s", scenario, buf[:n])
		}
	}
	return u, screen, checkAlive
}

func key(s tcell.SimulationScreen, k tcell.Key) {
	s.InjectKey(k, 0, tcell.ModNone)
	time.Sleep(120 * time.Millisecond)
}

func click(s tcell.SimulationScreen, x, y int) {
	s.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	s.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	time.Sleep(120 * time.Millisecond)
}

func TestClose_F10(t *testing.T) {
	_, s, alive := newTestUI(t)
	key(s, tcell.KeyF10)
	key(s, tcell.KeyF10) // закрыть тем же F10
	alive("F10→F10")
}

func TestClose_SelectItem(t *testing.T) {
	_, s, alive := newTestUI(t)
	key(s, tcell.KeyF10)   // меню «Файл»
	key(s, tcell.KeyDown)  // на пункт
	key(s, tcell.KeyEnter) // выбрать (Справка → диалог)
	alive("F10→↓→Enter")
}

func TestClose_NavThenEsc(t *testing.T) {
	_, s, alive := newTestUI(t)
	key(s, tcell.KeyF10)
	key(s, tcell.KeyRight) // соседнее меню
	key(s, tcell.KeyRight)
	key(s, tcell.KeyEscape)
	alive("F10→→→Esc")
}

func TestClose_ClickAway(t *testing.T) {
	_, s, alive := newTestUI(t)
	key(s, tcell.KeyF10) // открыть меню
	click(s, 5, 20)      // клик по панели чатов мимо меню
	alive("F10→клик мимо")
}

func TestDialog_F1(t *testing.T) {
	_, s, alive := newTestUI(t)
	key(s, tcell.KeyF1)    // открыть справку (диалог)
	key(s, tcell.KeyEnter) // закрыть (OK)
	alive("F1→Enter")
}

func TestDialog_FromMenuAndClose(t *testing.T) {
	_, s, alive := newTestUI(t)
	key(s, tcell.KeyF10)   // меню «Файл»
	key(s, tcell.KeyEnter) // первый пункт «Справка» → диалог
	key(s, tcell.KeyEscape) // закрыть диалог
	alive("F10→Enter→Esc")
}

func TestRepeatedMenu(t *testing.T) {
	_, s, alive := newTestUI(t)
	for i := 0; i < 5; i++ {
		key(s, tcell.KeyF10)
		key(s, tcell.KeyEscape)
	}
	alive("5×(F10→Esc)")
}

func TestContextMenu(t *testing.T) {
	_, s, alive := newTestUI(t)
	s.InjectKey(tcell.KeyF10, 0, tcell.ModShift) // Shift+F10 — контекстное меню
	time.Sleep(120 * time.Millisecond)
	key(s, tcell.KeyEscape)
	alive("Shift+F10→Esc")
}

// TestLiveUpdateWhileMenuOpen — живое сообщение приходит, пока открыто меню,
// затем меню закрывается. Имитирует listenUpdates через QueueUpdateDraw.
func TestLiveUpdateWhileMenuOpen(t *testing.T) {
	u, s, alive := newTestUI(t)
	key(s, tcell.KeyF10) // открыть меню
	u.app.QueueUpdateDraw(func() {
		u.history = append(u.history, telegram.HistoryMessage{ID: 3, Author: "Alice", Text: "ещё"})
		u.renderMessages()
	})
	time.Sleep(120 * time.Millisecond)
	key(s, tcell.KeyEscape) // закрыть меню
	alive("живое сообщение + Esc")
}

// TestFocusCycleWithData — Tab по всем панелям и обратно на реальных данных.
func TestFocusCycleWithData(t *testing.T) {
	_, s, alive := newTestUI(t)
	for i := 0; i < 6; i++ {
		key(s, tcell.KeyTab)
	}
	key(s, tcell.KeyF10)
	key(s, tcell.KeyEscape)
	alive("Tab×6 + меню")
}

package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

// TestViewFits проверяет, что View не переполняет терминал: число строк равно
// высоте, и ни одна строка не шире ширины. Переполнение = «глюки» alt-screen.
func TestViewFits(t *testing.T) {
	var sizes [][2]int
	for w := 40; w <= 200; w += 7 { // нечётный шаг — ловим граничные ширины
		for h := 14; h <= 60; h += 5 {
			sizes = append(sizes, [2]int{w, h})
		}
	}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		m := newModel(context.Background(), nil, nil, "v1.0")
		m.width, m.height = w, h
		m.loading = false
		m.resizeInput()
		m.dialogs = []telegram.Dialog{
			{Title: "Алиса Петрова", Kind: "user", Unread: 3, Ref: telegram.PeerRef{Type: "user", ID: 1}},
			{Title: "🐧 Puffin Cafe — Аудиокниги 🐧", Kind: "channel", Ref: telegram.PeerRef{Type: "channel", ID: 2}},
			{Title: "Рабочая группа", Kind: "group", Ref: telegram.PeerRef{Type: "chat", ID: 3}},
		}
		m.history = []telegram.HistoryMessage{
			{Author: "Алиса", Text: "привет, как дела с проектом?"},
			{Out: true, Text: "норм, заканчиваю"},
		}

		for _, show := range []struct{ l, r bool }{{true, false}, {true, true}, {false, false}} {
			m.showLeft, m.showRight = show.l, show.r
			m.resizeInput()
			out := m.View()
			lines := strings.Split(out, "\n")
			if len(lines) != h {
				t.Errorf("%dx%d left=%v right=%v: строк %d, ожидалось %d", w, h, show.l, show.r, len(lines), h)
			}
			for i, l := range lines {
				if lw := lipgloss.Width(l); lw > w {
					t.Errorf("%dx%d left=%v right=%v: строка %d шириной %d > %d", w, h, show.l, show.r, i, lw, w)
				}
			}
		}
	}
}

package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func benchModel() model {
	m := newModel(context.Background(), nil, nil, "v1.0")
	m.width, m.height = 120, 40
	m.loading = false
	m.resizeInput()
	for i := 0; i < 100; i++ {
		m.dialogs = append(m.dialogs, telegram.Dialog{
			Title: "Чат номер " + strings.Repeat("х", i%20), Kind: "user",
			Ref: telegram.PeerRef{Type: "user", ID: int64(i)},
		})
	}
	for i := 0; i < 50; i++ {
		m.history = append(m.history, telegram.HistoryMessage{Author: "Кто-то", Text: "сообщение текст текст"})
	}
	return m
}

// BenchmarkView измеряет стоимость одной перерисовки интерфейса.
func BenchmarkView(b *testing.B) {
	m := benchModel()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

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

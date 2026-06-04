package tui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

// TestMsgListFillsAndBg проверяет примитив ленты: при выборе верхнего сообщения
// панель заполняется целиком (без пустоты снизу), а у строк есть фон на всю
// ширину (а не только под текстом).
func TestMsgListFillsAndBg(t *testing.T) {
	u := &ui{app: tview.NewApplication(), selected: map[int]bool{}, treeWidth: 48,
		forumLoaded: map[string]bool{}, downloads: map[int64]*download{}, menuActive: -1}
	u.build()
	u.open = &telegram.Dialog{Title: "c", CanSend: true}
	for i := 0; i < 12; i++ {
		u.history = append(u.history, telegram.HistoryMessage{ID: int64(i), Author: "A", Text: fmt.Sprintf("msg-%02d", i)})
	}
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	w, h := 40, 8 // внутренняя высота 6 строк < 12 сообщений
	s.SetSize(w, h)
	u.messages.SetRect(0, 0, w, h)
	inner := h - 2

	for _, sel := range []int{11, 0, 6, 0} {
		u.msgSel = sel
		u.messages.Draw(s)
		s.Show()
		nonEmpty := 0
		fullWidthBG := false
		for y := 1; y < h-1; y++ {
			hasText := false
			for x := 1; x < w-1; x++ {
				if c, _, _, _ := s.GetContent(x, y); c != 0 && c != ' ' {
					hasText = true
				}
			}
			if hasText {
				nonEmpty++
				// фон у правого края строки (где текста нет) должен быть задан —
				// признак фона на всю ширину, а не только под текстом.
				if _, _, st, _ := s.GetContent(w-2, y); st != tcell.StyleDefault {
					fullWidthBG = true
				}
			}
		}
		if nonEmpty < inner {
			t.Errorf("sel=%d: панель не заполнена (%d/%d строк)", sel, nonEmpty, inner)
		}
		if !fullWidthBG {
			t.Errorf("sel=%d: нет фона на всю ширину", sel)
		}
	}
}

package tui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

// TestMessagesScrollNoBlank проверяет, что при прокрутке к любому сообщению
// (в т.ч. к самому верхнему) панель сообщений заполняется целиком, а не оставляет
// пустоту снизу — баг был из-за ScrollToHighlight, недопарсивавшего индекс.
func TestMessagesScrollNoBlank(t *testing.T) {
	u := &ui{app: tview.NewApplication(), selected: map[int]bool{}, menuActive: -1}
	u.build()
	u.open = &telegram.Dialog{Title: "c", CanSend: true}
	for i := 0; i < 12; i++ {
		u.history = append(u.history, telegram.HistoryMessage{ID: int64(i), Author: "A", Text: fmt.Sprintf("msg-%02d", i)})
	}
	s := tcell.NewSimulationScreen("")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	w, h := 30, 8 // внутренняя высота 6 строк < 12 сообщений
	s.SetSize(w, h)
	u.messages.SetRect(0, 0, w, h)
	inner := h - 2

	for _, sel := range []int{11, 0, 6, 0} {
		u.msgSel = sel
		u.renderMessages()
		u.messages.Draw(s)
		s.Show()
		nonEmpty, selVisible := 0, false
		for y := 1; y < h-1; y++ {
			line := ""
			for x := 1; x < w-1; x++ {
				if c, _, _, _ := s.GetContent(x, y); c != 0 && c != ' ' {
					line += string(c)
				}
			}
			if line != "" {
				nonEmpty++
			}
		}
		// выбранное сообщение должно быть в выводимом тексте
		raw := u.messages.GetText(true)
		selVisible = len(raw) > 0
		if nonEmpty < inner {
			t.Errorf("sel=%d: панель не заполнена (%d/%d строк), низ пустеет", sel, nonEmpty, inner)
		}
		if !selVisible {
			t.Errorf("sel=%d: пустой вывод", sel)
		}
	}
}

package tui

// msgList — собственный примитив ленты сообщений вместо tview.TextView. Рисует
// сообщения сам: фон на всю ширину панели, ровная зебра у соседних и сплошная
// полоса-курсор у выбранного, перенос длинных строк по словам, усечение слишком
// длинных сообщений (полный текст — по F3) и собственная прокрутка к выбранному.

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

// mglyph — символ с его стилем (цвет/начертание); фон задаётся строкой при отрисовке.
type mglyph struct {
	r  rune
	st tcell.Style
}

// mrow — отрисовываемая строка: фон на всю ширину и символы поверх него.
type mrow struct {
	bg     tcell.Color
	glyphs []mglyph
}

type msgList struct {
	*tview.Box
	ui          *ui
	offset      int    // прокрутка в строках
	placeholder string // текст-заглушка, когда сообщений нет (например, «Загрузка…»)
}

func newMsgList(u *ui) *msgList {
	return &msgList{Box: tview.NewBox(), ui: u}
}

func (m *msgList) Draw(screen tcell.Screen) {
	m.DrawForSubclass(screen, m)
	x, y, w, h := m.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	u := m.ui
	if len(u.history) == 0 {
		if m.placeholder != "" {
			tview.Print(screen, m.placeholder, x, y, w, tview.AlignLeft, tcell.GetColor(theme.TextDim))
		}
		return
	}

	rows, selStart, selEnd := m.layout(w, h)

	// Прокрутка: держим выбранное сообщение в окне (минимальными сдвигами).
	if selStart < m.offset {
		m.offset = selStart
	}
	if selEnd > m.offset+h-1 {
		m.offset = selEnd - h + 1
	}
	if m.offset > len(rows)-h {
		m.offset = len(rows) - h
	}
	if m.offset < 0 {
		m.offset = 0
	}

	for i := 0; i < h; i++ {
		ri := m.offset + i
		if ri >= len(rows) {
			break
		}
		drawMRow(screen, x, y+i, w, rows[ri])
	}
	// Скроллбар на правой рамке.
	bx, by, bw, bh := m.GetRect()
	drawScrollbar(screen, bx+bw-1, by+1, bh-2, m.offset, len(rows), h)
}

// layout раскладывает все сообщения в строки шириной w, усекая слишком длинные.
// Возвращает строки и диапазон строк выбранного сообщения.
func (m *msgList) layout(w, h int) (rows []mrow, selStart, selEnd int) {
	u := m.ui
	maxLines := h / 3 // слишком длинные (> трети высоты) сокращаем
	if maxLines < 4 {
		maxLines = 4
	}
	for i := range u.history {
		bg := m.bgFor(i)
		grows := wrapGlyphs(u.messageGlyphs(i), w)
		if len(grows) > maxLines {
			grows = grows[:maxLines-1]
			grows = append(grows, glyphsOf("  … (F3 — открыть целиком)", tcell.StyleDefault.Foreground(tcell.GetColor(theme.TextDim))))
		}
		start := len(rows)
		for _, gr := range grows {
			rows = append(rows, mrow{bg: bg, glyphs: gr})
		}
		if i == u.msgSel {
			selStart, selEnd = start, len(rows)-1
		}
	}
	return rows, selStart, selEnd
}

// bgFor — фон сообщения: выбранное — полоса-курсор, остальные — зебра по чётности.
func (m *msgList) bgFor(i int) tcell.Color {
	if i == m.ui.msgSel {
		return tcell.GetColor(theme.MsgSel)
	}
	if i%2 == 1 {
		return tcell.GetColor(theme.MsgBgAlt)
	}
	return tcell.GetColor(theme.MsgBg)
}

func drawMRow(screen tcell.Screen, x, y, w int, row mrow) {
	bgStyle := tcell.StyleDefault.Background(row.bg)
	for cx := x; cx < x+w; cx++ {
		screen.SetContent(cx, y, ' ', nil, bgStyle)
	}
	cx := x
	for _, gl := range row.glyphs {
		gw := runewidth.RuneWidth(gl.r)
		if gw == 0 { // нулевая ширина (комбинирующие/невидимые) — пропускаем
			continue
		}
		if cx+gw > x+w { // широкий символ не влезает у края
			break
		}
		screen.SetContent(cx, y, gl.r, nil, gl.st.Background(row.bg))
		cx += gw
	}
}

func glyphsOf(s string, st tcell.Style) []mglyph {
	g := make([]mglyph, 0, len(s))
	for _, r := range s {
		g = append(g, mglyph{r, st})
	}
	return g
}

// rowWidth — ширина строки глифов в клетках терминала (учёт широких символов).
func rowWidth(g []mglyph) int {
	w := 0
	for _, gl := range g {
		w += runewidth.RuneWidth(gl.r)
	}
	return w
}

// wrapGlyphs переносит символы по ширине w (в КЛЕТКАХ терминала — эмодзи/CJK
// занимают 2), предпочитая разрыв по пробелу; '\n' начинает новую строку.
// Символы нулевой ширины (комбинирующие/невидимые) отбрасываются — защита от
// перекоса вывода.
func wrapGlyphs(g []mglyph, w int) [][]mglyph {
	if w < 1 {
		w = 1
	}
	var rows [][]mglyph
	cur := []mglyph{}
	curW := 0
	for _, gl := range g {
		if gl.r == '\n' {
			rows = append(rows, cur)
			cur = []mglyph{}
			curW = 0
			continue
		}
		gw := runewidth.RuneWidth(gl.r)
		if gw == 0 {
			continue
		}
		if curW+gw > w {
			// Ищем последний пробел в хвосте, чтобы не рвать слово посередине.
			br := -1
			for k := len(cur) - 1; k >= 0; k-- {
				if cur[k].r == ' ' {
					br = k
					break
				}
			}
			if br > 0 && br > len(cur)*2/3 { // пробел не слишком далеко от края
				rows = append(rows, cur[:br])
				cur = append([]mglyph{}, cur[br+1:]...)
			} else {
				rows = append(rows, cur)
				cur = []mglyph{}
			}
			curW = rowWidth(cur)
		}
		cur = append(cur, gl)
		curW += gw
	}
	rows = append(rows, cur)
	return rows
}

// messageGlyphs превращает сообщение в плоский поток символов со стилями
// (время, автор, форматированное тело, строка вложения).
func (u *ui) messageGlyphs(i int) []mglyph {
	msg := u.history[i]
	var g []mglyph
	push := func(s string, st tcell.Style) { g = append(g, glyphsOf(s, st)...) }

	push(msg.Date.Format("15:04")+" ", tcell.StyleDefault.Foreground(tcell.GetColor(theme.TextDim)))
	if msg.Out {
		push("→ ", tcell.StyleDefault.Foreground(tcell.GetColor(theme.MsgOut)))
	} else {
		push(msg.Author+": ", tcell.StyleDefault.Foreground(tcell.GetColor(theme.MsgAuthor)).Bold(true))
	}
	yellow := tcell.StyleDefault.Foreground(tview.Styles.PrimaryTextColor)
	if len(msg.Spans) == 0 {
		push(msg.Text, yellow)
	} else {
		for _, s := range msg.Spans {
			st := yellow
			if s.B {
				st = st.Bold(true)
			}
			if s.I {
				st = st.Italic(true)
			}
			if s.S {
				st = st.StrikeThrough(true)
			}
			if s.Code {
				st = tcell.StyleDefault.Foreground(tcell.GetColor(theme.MsgCode))
			}
			if s.URL != "" {
				st = tcell.StyleDefault.Foreground(tcell.GetColor(theme.MsgLink)).Underline(true)
			}
			push(s.Text, st)
		}
	}
	if msg.Media != nil {
		push("\n📎 "+msg.Media.Label(), tcell.StyleDefault.Foreground(tcell.GetColor(theme.MsgCode)))
	}
	return g
}

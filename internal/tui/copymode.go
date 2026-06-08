package tui

// copyView — режим выделения и копирования части текста сообщения («v» в ленте).
// Показывает текст сообщения с подвижным блоком-курсором; «v» или пробел ставит
// начало выделения, движение расширяет его, «y»/Enter копирует выделенное (а без
// выделения — текущую строку). Esc — выход. Движение: стрелки или hjkl, w/b —
// по словам, Home/End — края строки, g/G — начало/конец текста.
//
// Текст хранится как срез рун с сохранением оригинальных переводов строк, а
// курсор — это смещение в этом срезе. Видимые строки (после переноса по ширине)
// держатся отдельной картой сегментов, поэтому выделение всегда копируется ровно
// тем текстом, что в сообщении (мягкие переносы не попадают в результат).

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

// textSeg — видимая строка: полуинтервал [start, end) в рунах текста (без
// завершающего пробела мягкого переноса и без символа '\n').
type textSeg struct{ start, end int }

type copyView struct {
	*tview.Box
	ui    *ui
	text  []rune    // полный текст сообщения (с оригинальными '\n')
	segs  []textSeg // видимые строки после переноса
	wrapW int       // ширина, под которую посчитаны segs
	pos   int       // курсор: смещение в text
	anc   int       // якорь выделения; -1 — выделения нет
	off   int       // прокрутка по строкам
}

func newCopyView(u *ui, msg telegram.HistoryMessage) *copyView {
	c := &copyView{Box: tview.NewBox(), ui: u, text: []rune(msg.Plain()), anc: -1}
	title := msg.Author
	if msg.Out {
		title = "Вы"
	}
	c.SetBorder(true).SetTitle(" Выделение — v выделить, y копировать, Esc выход │ " + tview.Escape(title) + " ")
	c.SetBorderColor(tcell.GetColor(theme.BorderActive))
	c.SetTitleColor(tcell.GetColor(theme.TitleActive))
	c.SetBackgroundColor(tcell.GetColor(theme.BgPanel))
	return c
}

// wrapText переносит текст по ширине w (в клетках; эмодзи/CJK = 2), предпочитая
// разрыв по пробелу; '\n' начинает новую видимую строку. Возвращает сегменты —
// полуинтервалы в рунах text.
func wrapText(text []rune, w int) []textSeg {
	if w < 1 {
		w = 1
	}
	var segs []textSeg
	n := len(text)
	i, lineStart, curW, lastSpace := 0, 0, 0, -1
	for i < n {
		r := text[i]
		if r == '\n' {
			segs = append(segs, textSeg{lineStart, i})
			i++
			lineStart, curW, lastSpace = i, 0, -1
			continue
		}
		rw := runewidth.RuneWidth(r)
		if rw == 0 { // комбинирующие/невидимые — ширины не добавляют
			i++
			continue
		}
		if curW+rw > w && i > lineStart {
			if r == ' ' { // перенос ровно на пробеле — пробел пропускаем
				segs = append(segs, textSeg{lineStart, i})
				i++
				lineStart, curW, lastSpace = i, 0, -1
				continue
			}
			if lastSpace > lineStart { // разрыв по пробелу — сам пробел пропускаем
				segs = append(segs, textSeg{lineStart, lastSpace})
				lineStart = lastSpace + 1
			} else { // длинное слово — жёсткий разрыв перед текущим символом
				segs = append(segs, textSeg{lineStart, i})
				lineStart = i
			}
			curW = 0
			for k := lineStart; k < i; k++ {
				curW += runewidth.RuneWidth(text[k])
			}
			lastSpace = -1
			continue // символ i ещё не учтён
		}
		if r == ' ' {
			lastSpace = i
		}
		curW += rw
		i++
	}
	segs = append(segs, textSeg{lineStart, n})
	return segs
}

func (c *copyView) rewrap(w int) {
	if w == c.wrapW && c.segs != nil {
		return
	}
	c.wrapW = w
	c.segs = wrapText(c.text, w)
}

// locate возвращает индекс видимой строки и колонку (в рунах) для смещения pos.
func (c *copyView) locate(pos int) (line, col int) {
	for i, s := range c.segs {
		if pos < s.start { // в «дыре» переноса — относим к концу предыдущей строки
			if i > 0 {
				return i - 1, c.segs[i-1].end - c.segs[i-1].start
			}
			return 0, 0
		}
		if pos <= s.end {
			return i, pos - s.start
		}
	}
	last := len(c.segs) - 1
	if last < 0 {
		return 0, 0
	}
	return last, c.segs[last].end - c.segs[last].start
}

// posAt — смещение в text для строки line и колонки col (с клампом по строке).
func (c *copyView) posAt(line, col int) int {
	if line < 0 {
		line = 0
	}
	if line >= len(c.segs) {
		line = len(c.segs) - 1
	}
	s := c.segs[line]
	p := s.start + col
	if p < s.start {
		p = s.start
	}
	if p > s.end {
		p = s.end
	}
	return p
}

func (c *copyView) clamp() {
	if c.pos < 0 {
		c.pos = 0
	}
	if max := len(c.text) - 1; c.pos > max {
		c.pos = max
	}
	if c.pos < 0 {
		c.pos = 0
	}
}

func (c *copyView) moveLine(d int) {
	line, col := c.locate(c.pos)
	c.pos = c.posAt(line+d, col)
	c.clamp()
}

func (c *copyView) home() {
	line, _ := c.locate(c.pos)
	c.pos = c.segs[line].start
}

func (c *copyView) end() {
	line, _ := c.locate(c.pos)
	if s := c.segs[line]; s.end > s.start {
		c.pos = s.end - 1
	} else {
		c.pos = s.start
	}
	c.clamp()
}

// wordFwd/wordBack двигают курсор к началу следующего/предыдущего слова.
func (c *copyView) wordFwd() {
	n := len(c.text)
	i := c.pos
	for i < n && !isSpace(c.text[i]) {
		i++
	}
	for i < n && isSpace(c.text[i]) {
		i++
	}
	c.pos = i
	c.clamp()
}

func (c *copyView) wordBack() {
	i := c.pos - 1
	for i > 0 && isSpace(c.text[i]) {
		i--
	}
	for i > 0 && !isSpace(c.text[i-1]) {
		i--
	}
	if i < 0 {
		i = 0
	}
	c.pos = i
}

func isSpace(r rune) bool { return r == ' ' || r == '\n' || r == '\t' }

// enterCopyMode открывает режим выделения для выбранного сообщения. Если в
// сообщении нет текста (только вложение) — выделять нечего.
func (u *ui) enterCopyMode() {
	if u.msgSel < 0 || u.msgSel >= len(u.history) {
		return
	}
	cv := newCopyView(u, u.history[u.msgSel])
	if len(cv.text) == 0 {
		u.status.SetText("[" + theme.Warn + "]В сообщении нет текста для выделения[-]  " + msgHints())
		return
	}
	cv.pos = len(cv.text) - 1
	u.pages.AddPage("copy", centeredFrac(cv), true, true)
	u.app.SetFocus(cv)
}

func (u *ui) closeCopyMode() {
	u.pages.RemovePage("copy")
	u.app.SetFocus(u.messages)
}

// centeredFrac размещает примитив по центру экрана, занимая ~3/4 ширины и высоты
// (пропорциональные nil-отступы оставляют интерфейс под ним видимым).
func centeredFrac(p tview.Primitive) tview.Primitive {
	col := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(p, 0, 6, true).
		AddItem(nil, 0, 1, false)
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(col, 0, 6, true).
		AddItem(nil, 0, 1, false)
}

func (c *copyView) toggleAnchor() {
	if c.anc < 0 {
		c.anc = c.pos
	} else {
		c.anc = -1
	}
}

// selRange возвращает выделенный диапазон [lo, hi] (включительно) или ok=false.
func (c *copyView) selRange() (lo, hi int, ok bool) {
	if c.anc < 0 {
		return 0, 0, false
	}
	lo, hi = c.anc, c.pos
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi >= len(c.text) {
		hi = len(c.text) - 1
	}
	return lo, hi, lo <= hi
}

// copy копирует выделение (или текущую видимую строку, если выделения нет) и
// закрывает режим.
func (c *copyView) copy() {
	var s string
	if lo, hi, ok := c.selRange(); ok {
		s = string(c.text[lo : hi+1])
	} else {
		line, _ := c.locate(c.pos)
		seg := c.segs[line]
		s = string(c.text[seg.start:seg.end])
	}
	c.ui.closeCopyMode()
	if s == "" {
		return
	}
	c.ui.copyToClipboard(s, "["+theme.Success+"]Скопировано выделенное[-]  "+msgHints())
}

func (c *copyView) Draw(screen tcell.Screen) {
	c.DrawForSubclass(screen, c)
	x, y, w, h := c.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	c.rewrap(w)
	c.clamp()

	// Держим строку курсора в окне.
	curLine, _ := c.locate(c.pos)
	if curLine < c.off {
		c.off = curLine
	}
	if curLine >= c.off+h {
		c.off = curLine - h + 1
	}
	if c.off > len(c.segs)-h {
		c.off = len(c.segs) - h
	}
	if c.off < 0 {
		c.off = 0
	}

	lo, hi, hasSel := c.selRange()
	base := tcell.StyleDefault.
		Background(tcell.GetColor(theme.BgPanel)).
		Foreground(tcell.GetColor(theme.Text))
	selSt := tcell.StyleDefault.
		Background(tcell.GetColor(theme.MsgSel)).
		Foreground(tcell.GetColor(theme.TextBright))
	curSt := tcell.StyleDefault.
		Background(tcell.GetColor(theme.BorderActive)).
		Foreground(tcell.GetColor(theme.Inverse))

	for row := 0; row < h; row++ {
		li := c.off + row
		if li >= len(c.segs) {
			break
		}
		seg := c.segs[li]
		cx := x
		drew := false
		for idx := seg.start; idx < seg.end; idx++ {
			r := c.text[idx]
			rw := runewidth.RuneWidth(r)
			if rw == 0 {
				continue
			}
			if cx+rw > x+w {
				break
			}
			st := base
			if hasSel && idx >= lo && idx <= hi {
				st = selSt
			}
			if idx == c.pos {
				st = curSt
				drew = true
			}
			screen.SetContent(cx, y+row, r, nil, st)
			cx += rw
		}
		if li == curLine && !drew && cx < x+w { // курсор на конце/пустой строке
			screen.SetContent(cx, y+row, ' ', nil, curSt)
		}
	}

	bx, by, bw, bh := c.GetRect()
	drawScrollbar(screen, bx+bw-1, by+1, bh-2, c.off, len(c.segs), h)
}

func (c *copyView) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return c.WrapInputHandler(func(ev *tcell.EventKey, _ func(tview.Primitive)) {
		switch ev.Key() {
		case tcell.KeyEscape:
			c.ui.closeCopyMode()
			return
		case tcell.KeyEnter:
			c.copy()
			return
		case tcell.KeyLeft:
			c.pos--
			c.clamp()
			return
		case tcell.KeyRight:
			c.pos++
			c.clamp()
			return
		case tcell.KeyUp:
			c.moveLine(-1)
			return
		case tcell.KeyDown:
			c.moveLine(1)
			return
		case tcell.KeyHome:
			c.home()
			return
		case tcell.KeyEnd:
			c.end()
			return
		}
		switch ev.Rune() {
		case 'h':
			c.pos--
			c.clamp()
		case 'l':
			c.pos++
			c.clamp()
		case 'k':
			c.moveLine(-1)
		case 'j':
			c.moveLine(1)
		case 'w':
			c.wordFwd()
		case 'b':
			c.wordBack()
		case 'g':
			c.pos = 0
		case 'G':
			c.pos = len(c.text) - 1
			c.clamp()
		case 'v', ' ':
			c.toggleAnchor()
		case 'y':
			c.copy()
		}
	})
}

package tui

// msgList — собственный примитив ленты сообщений вместо tview.TextView. Рисует
// сообщения сам: фон на всю ширину панели, ровная зебра у соседних и сплошная
// полоса-курсор у выбранного, перенос длинных строк по словам, усечение слишком
// длинных сообщений (полный текст — по F3) и собственная прокрутка к выбранному.

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

// mglyph — символ с его стилем (цвет/начертание); фон задаётся строкой при отрисовке.
type mglyph struct {
	r  rune
	st tcell.Style
}

// mrow — отрисовываемая строка кеша: глифы и индекс сообщения, которому строка
// принадлежит (фон — зебра/курсор — вычисляется при отрисовке по msgSel, чтобы
// смена выбранного сообщения не требовала пересборки раскладки).
type mrow struct {
	msg    int // индекс сообщения в u.history
	glyphs []mglyph
}

type msgList struct {
	*tview.Box
	ui          *ui
	offset      int    // прокрутка в строках
	placeholder string // текст-заглушка, когда сообщений нет (например, «Загрузка…»)

	// Кеш раскладки. Пересобирается только при смене ширины/высоты, изменении
	// истории (invalidate) или темы — а не на каждый кадр. Это снимает нагрузку
	// O(всех сообщений) с перерисовки при тысячах сообщений после докрутки.
	rows           []mrow
	rowStart       []int // индекс первой строки каждого сообщения в rows
	cacheW         int   // ширина, под которую собран кеш
	cacheH         int   // высота, под которую собран кеш
	dirty          bool  // история/тема изменились — пересобрать кеш
	savedScreenRow int   // экранная строка выбранного сообщения перед докруткой (для плавности)
}

func newMsgList(u *ui) *msgList {
	return &msgList{Box: tview.NewBox(), ui: u, dirty: true}
}

// invalidate помечает кеш раскладки на пересборку (история изменилась или сменена
// тема). Сама пересборка происходит лениво при следующей отрисовке.
func (m *msgList) invalidate() { m.dirty = true }

// ensureLayout пересобирает кеш строк, если он устарел (изменились размеры панели
// или содержимое/тема). Иначе использует готовый кеш.
func (m *msgList) ensureLayout(w, h int) {
	if !m.dirty && m.cacheW == w && m.cacheH == h {
		return
	}
	m.layout(w, h)
	m.cacheW, m.cacheH, m.dirty = w, h, false
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

	m.ensureLayout(w, h)
	rows := m.rows

	// Диапазон строк выбранного сообщения — из карты rowStart (без пересборки).
	selStart, selEnd := 0, 0
	if sel := u.msgSel; sel >= 0 && sel < len(m.rowStart) {
		selStart = m.rowStart[sel]
		if sel+1 < len(m.rowStart) {
			selEnd = m.rowStart[sel+1] - 1
		} else {
			selEnd = len(rows) - 1
		}
	}

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
		drawMRow(screen, x, y+i, w, m.bgFor(rows[ri].msg), rows[ri].glyphs)
	}
	// Скроллбар на правой рамке.
	bx, by, bw, bh := m.GetRect()
	drawScrollbar(screen, bx+bw-1, by+1, bh-2, m.offset, len(rows), h)
}

// layout раскладывает все сообщения в строки шириной w и сохраняет их в кеш
// (m.rows) вместе с картой начала каждого сообщения (m.rowStart). Каждое
// сообщение — двустрочный блок: шапка (автор слева, время справа) и тело с
// отступом ниже; слишком длинное тело усекается (полный текст — по F3).
func (m *msgList) layout(w, h int) {
	u := m.ui
	maxLines := h / 3 // слишком длинное тело (> трети высоты) сокращаем
	if maxLines < 4 {
		maxLines = 4
	}
	const indent = "   "
	indW := runewidth.StringWidth(indent)
	dimSt := tcell.StyleDefault.Foreground(tcell.GetColor(theme.TextDim))
	m.rows = m.rows[:0]
	m.rowStart = m.rowStart[:0]
	for i := range u.history {
		m.rowStart = append(m.rowStart, len(m.rows))
		m.rows = append(m.rows, mrow{msg: i, glyphs: m.headerGlyphs(i, w)})
		body := wrapGlyphs(u.bodyGlyphs(i), w-indW)
		if len(body) > maxLines {
			body = body[:maxLines-1]
			body = append(body, glyphsOf("… (F3 — открыть целиком)", dimSt))
		}
		for _, gr := range body {
			row := append(glyphsOf(indent, tcell.StyleDefault), gr...)
			m.rows = append(m.rows, mrow{msg: i, glyphs: row})
		}
	}
}

// headerGlyphs строит строку-шапку сообщения: имя автора слева (исходящее — «Вы»),
// справа прижаты дата со временем (с секундами) и значок статуса доставки.
func (m *msgList) headerGlyphs(i, w int) []mglyph {
	msg := m.ui.history[i]
	author := msg.Author
	authSt := tcell.StyleDefault.Foreground(tcell.GetColor(theme.MsgAuthor)).Bold(true)
	if msg.Out {
		author = "Вы"
		authSt = tcell.StyleDefault.Foreground(tcell.GetColor(theme.MsgOut)).Bold(true)
	}
	// Правая часть: «дата время:с» и (для исходящих) значок статуса.
	right := glyphsOf(msg.Date.Format("02.01.2006 15:04:05"),
		tcell.StyleDefault.Foreground(tcell.GetColor(theme.TextDim)))
	if msg.Out {
		if st := statusGlyphs(msg.Status); len(st) > 0 {
			right = append(right, mglyph{' ', tcell.StyleDefault})
			right = append(right, st...)
		}
	}
	rightW := rowWidth(right)

	maxAuthor := w - rightW - 1 // место под автора, пробел и правую часть
	if maxAuthor < 1 {
		maxAuthor = 1
	}
	author = runewidth.Truncate(strings.TrimSpace(author), maxAuthor, "…")
	gap := w - runewidth.StringWidth(author) - rightW
	if gap < 1 {
		gap = 1
	}
	g := glyphsOf(author, authSt)
	g = append(g, glyphsOf(strings.Repeat(" ", gap), tcell.StyleDefault)...)
	g = append(g, right...)
	return g
}

// statusGlyphs — значок статуса доставки исходящего сообщения: ⧖ отправляется,
// ✓ отправлено, ✓✓ прочитано, ✗ ошибка.
func statusGlyphs(s telegram.MsgStatus) []mglyph {
	switch s {
	case telegram.StatusSending:
		return glyphsOf("⧖", tcell.StyleDefault.Foreground(tcell.GetColor(theme.TextDim)))
	case telegram.StatusSent:
		return glyphsOf("✓", tcell.StyleDefault.Foreground(tcell.GetColor(theme.TextDim)))
	case telegram.StatusRead:
		return glyphsOf("✓✓", tcell.StyleDefault.Foreground(tcell.GetColor(theme.Info)).Bold(true))
	case telegram.StatusError:
		return glyphsOf("✗", tcell.StyleDefault.Foreground(tcell.GetColor(theme.ErrorC)).Bold(true))
	}
	return nil
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

func drawMRow(screen tcell.Screen, x, y, w int, bg tcell.Color, glyphs []mglyph) {
	bgStyle := tcell.StyleDefault.Background(bg)
	for cx := x; cx < x+w; cx++ {
		screen.SetContent(cx, y, ' ', nil, bgStyle)
	}
	cx := x
	for _, gl := range glyphs {
		gw := runewidth.RuneWidth(gl.r)
		if gw == 0 { // нулевая ширина (комбинирующие/невидимые) — пропускаем
			continue
		}
		if cx+gw > x+w { // широкий символ не влезает у края
			break
		}
		screen.SetContent(cx, y, gl.r, nil, gl.st.Background(bg))
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

// bodyGlyphs превращает тело сообщения в поток символов со стилями
// (форматированный текст и строка вложения) — без времени и автора, они в шапке.
func (u *ui) bodyGlyphs(i int) []mglyph {
	msg := u.history[i]
	var g []mglyph
	push := func(s string, st tcell.Style) { g = append(g, glyphsOf(s, st)...) }

	base := tcell.StyleDefault.Foreground(tcell.GetColor(theme.Text))
	if len(msg.Spans) == 0 {
		push(msg.Text, base)
	} else {
		for _, s := range msg.Spans {
			st := base
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
		if len(g) > 0 {
			push("\n", base)
		}
		push("📎 "+msg.Media.Label(), tcell.StyleDefault.Foreground(tcell.GetColor(theme.MsgCode)))
	}
	return g
}

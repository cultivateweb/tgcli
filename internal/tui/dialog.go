package tui

// Модальные окна в стиле Turbo Vision: своя замена tview.Modal с тенью,
// заголовком в рамке и кнопками [ OK ] / [ Отмена ] [ Да ]. Заведено отдельным
// примитивом, потому что tview.Modal центрируется внутри себя и не даёт
// пристроить тень (его frame приватный).
//
// ВАЖНО про deadlock (см. [[tgcli-next-session]]): закрытие диалога и возврат
// фокуса идут только из обработчиков ввода/мыши — они выполняются без мьютекса
// приложения. Никакого SetFocus из Blur-колбэка здесь нет.

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// dialog — модальное окно: рамка с заголовком, строки текста по центру и ряд
// кнопок снизу. onDone получает индекс нажатой кнопки или -1 при отмене/Esc.
type dialog struct {
	*tview.Box
	lines   []string
	buttons []string
	active  int      // индекс кнопки в фокусе
	btnX    [][2]int // X-диапазоны кнопок (для мыши), параллельно buttons
	btnY    int      // Y строки кнопок (для мыши)
	onDone  func(idx int)
}

func newDialog(title string, lines, buttons []string, onDone func(int)) *dialog {
	d := &dialog{
		Box:     tview.NewBox(),
		lines:   lines,
		buttons: buttons,
		onDone:  onDone,
	}
	d.SetBorder(true).SetTitle(" " + title + " ")
	d.SetBorderColor(tcell.GetColor(theme.BorderActive))
	d.SetTitleColor(tcell.GetColor(theme.TitleActive))
	// По умолчанию фокус на первой кнопке: для confirm это «Отмена» —
	// безопасно при необратимых действиях (Enter не удаляет случайно).
	return d
}

// buttonsWidth — суммарная ширина ряда кнопок: каждая «[ label ]» плюс по 2
// пробела между соседними.
func (d *dialog) buttonsWidth() int {
	w := 0
	for i, b := range d.buttons {
		w += len([]rune(b)) + 4 // "[ " + label + " ]"
		if i > 0 {
			w += 2
		}
	}
	return w
}

// size вычисляет внешние ширину и высоту окна по тексту и кнопкам.
func (d *dialog) size() (int, int) {
	content := d.buttonsWidth()
	for _, l := range d.lines {
		if n := len([]rune(l)); n > content {
			content = n
		}
	}
	if t := len([]rune(d.GetTitle())); t > content {
		content = t
	}
	w := content + 4          // 1 паддинг + 1 рамка с каждой стороны
	h := len(d.lines) + 2 + 2 // строки + пустая + кнопки, плюс рамка сверху/снизу
	return w, h
}

func (d *dialog) Draw(screen tcell.Screen) {
	w, h := d.size()
	sw, sh := screen.Size()
	d.SetRect((sw-w)/2, (sh-h)/2, w, h)
	d.DrawForSubclass(screen, d)

	ix, iy, iw, _ := d.GetInnerRect()
	textStyle := tcell.StyleDefault.
		Background(tview.Styles.PrimitiveBackgroundColor).
		Foreground(tview.Styles.PrimaryTextColor)
	for i, line := range d.lines {
		drawCentered(screen, ix, iy+i, iw, line, textStyle)
	}

	d.btnY = iy + len(d.lines) + 1
	d.drawButtons(screen, ix, d.btnY, iw)
}

// drawButtons рисует ряд кнопок по центру; активная — чёрным на cyan,
// остальные — белым на синем фоне окна. Заодно запоминает X-диапазоны для мыши.
func (d *dialog) drawButtons(screen tcell.Screen, x, y, width int) {
	total := d.buttonsWidth()
	col := x
	if total < width {
		col = x + (width-total)/2
	}
	d.btnX = d.btnX[:0]
	for i, b := range d.buttons {
		if i > 0 {
			col += 2
		}
		style := tcell.StyleDefault.
			Background(tview.Styles.PrimitiveBackgroundColor).
			Foreground(tcell.GetColor(theme.MenuText))
		if i == d.active {
			style = tcell.StyleDefault.
				Background(tcell.GetColor(theme.MenuSelBg)).
				Foreground(tcell.GetColor(theme.MenuSelFg))
		}
		start := col
		for _, ch := range "[ " + b + " ]" {
			screen.SetContent(col, y, ch, nil, style)
			col++
		}
		d.btnX = append(d.btnX, [2]int{start, col})
	}
}

func (d *dialog) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return d.WrapInputHandler(func(ev *tcell.EventKey, _ func(tview.Primitive)) {
		switch ev.Key() {
		case tcell.KeyEscape:
			d.onDone(-1)
		case tcell.KeyEnter:
			d.onDone(d.active)
		case tcell.KeyTab, tcell.KeyRight:
			d.active = (d.active + 1) % len(d.buttons)
		case tcell.KeyBacktab, tcell.KeyLeft:
			d.active = (d.active - 1 + len(d.buttons)) % len(d.buttons)
		case tcell.KeyRune:
			if ev.Rune() == ' ' {
				d.onDone(d.active)
			}
		}
	})
}

func (d *dialog) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return d.WrapMouseHandler(func(action tview.MouseAction, ev *tcell.EventMouse, _ func(tview.Primitive)) (bool, tview.Primitive) {
		x, y := ev.Position()
		if action == tview.MouseLeftClick {
			for i, r := range d.btnX {
				if y == d.btnY && x >= r[0] && x < r[1] {
					d.onDone(i)
					return true, nil
				}
			}
		}
		// Окно модальное: поглощаем все события мыши, чтобы клики не уходили на
		// панели под ним (иначе фокус ушёл бы, а окно осталось бы висеть).
		return true, nil
	})
}

// drawCentered рисует строку s по центру отрезка [x, x+width) на строке y.
func drawCentered(screen tcell.Screen, x, y, width int, s string, style tcell.Style) {
	r := []rune(s)
	col := x
	if len(r) < width {
		col = x + (width-len(r))/2
	}
	for _, ch := range r {
		if col >= x+width {
			break
		}
		screen.SetContent(col, y, ch, nil, style)
		col++
	}
}

// showDialog показывает модальный диалог поверх интерфейса. choose получает
// индекс нажатой кнопки или -1 при отмене/Esc. По завершении окно убирается и
// фокус возвращается последней активной панели.
func (u *ui) showDialog(title, text string, buttons []string, choose func(idx int)) {
	d := newDialog(title, strings.Split(text, "\n"), buttons, func(idx int) {
		u.pages.RemovePage("dialog")
		if u.lastPanel != nil {
			u.app.SetFocus(u.lastPanel)
		} else {
			u.app.SetFocus(u.tree)
		}
		if choose != nil {
			choose(idx)
		}
	})
	// resize=false: окно рисуется в своём rect (его задаёт Draw), интерфейс под
	// ним остаётся виден — так же, как выпадающие меню.
	u.pages.AddPage("dialog", shadowed{d}, false, true)
	u.app.SetFocus(d)
}

// showPrompt показывает модальное окно с однострочным полем ввода. onDone
// получает введённый текст и ok=true при Enter с непустым текстом; ok=false при
// Esc/отмене. Используется для ввода имени закладки (Ctrl+D).
func (u *ui) showPrompt(title, initial string, onDone func(text string, ok bool)) {
	in := tview.NewInputField().SetText(initial)
	in.SetBorder(true).SetTitle(" " + title + " ")
	in.SetBorderColor(tcell.GetColor(theme.BorderActive))
	in.SetTitleColor(tcell.GetColor(theme.TitleActive))
	in.SetFieldBackgroundColor(tcell.GetColor(theme.BgPanel))
	in.SetFieldTextColor(tcell.GetColor(theme.Text))
	in.SetDoneFunc(func(key tcell.Key) {
		text := strings.TrimSpace(in.GetText())
		u.pages.RemovePage("prompt")
		if u.lastPanel != nil {
			u.app.SetFocus(u.lastPanel)
		} else {
			u.app.SetFocus(u.tree)
		}
		onDone(text, key == tcell.KeyEnter && text != "")
	})
	u.pages.AddPage("prompt", centered(in, 54, 3), true, true)
	u.app.SetFocus(in)
}

// centered размещает примитив p размером w×h по центру экрана, оставляя вокруг
// прозрачные отступы (через nil-ячейки Flex интерфейс под ним остаётся виден).
func centered(p tview.Primitive, w, h int) tview.Primitive {
	col := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(p, h, 0, true).
		AddItem(nil, 0, 1, false)
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(col, w, 0, true).
		AddItem(nil, 0, 1, false)
}

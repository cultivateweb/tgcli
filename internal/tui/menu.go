package tui

// Верхнее меню в стиле CUA / Turbo Pascal: F10 активирует меню-бар, Alt+буква
// открывает конкретный пункт, ← → переключают соседние меню, ↑↓ выбирают
// действие, Enter выполняет, Esc закрывает. Работает и мышью.
//
// Состояние «какое меню открыто» хранит подсветка региона меню-бара
// (u.header): и клик мышью, и клавиши лишь меняют подсветку, а реакция —
// в SetHighlightedFunc (открыть/закрыть выпадение). Так мышь и клавиатура
// идут через одну точку.

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// shadowed оборачивает любой примитив, добавляя тень Turbo Vision справа и
// снизу. Фокус и ввод идут к вложенному примитиву напрямую; обёртка лишь
// дорисовывает тень после него.
type shadowed struct{ tview.Primitive }

func (s shadowed) Draw(screen tcell.Screen) {
	s.Primitive.Draw(screen)
	x, y, w, h := s.Primitive.GetRect()
	drawShadow(screen, x, y, w, h)
}

// drawShadow затемняет ячейки справа (2 колонки) и снизу (1 строка) от окна —
// фирменная тень Turbo Vision.
func drawShadow(screen tcell.Screen, x, y, w, h int) {
	sw, sh := screen.Size()
	style := tcell.StyleDefault.
		Background(tcell.GetColor(theme.Shadow)).
		Foreground(tcell.GetColor(theme.ShadowFg))
	dim := func(cx, cy int) {
		if cx < 0 || cy < 0 || cx >= sw || cy >= sh {
			return
		}
		r, _, _, _ := screen.GetContent(cx, cy)
		screen.SetContent(cx, cy, r, nil, style)
	}
	for cy := y + 1; cy < y+h+1; cy++ { // правая кромка
		dim(x+w, cy)
		dim(x+w+1, cy)
	}
	for cx := x + 2; cx < x+w+2; cx++ { // нижняя кромка
		dim(cx, y+h)
	}
}

type menuItem struct {
	label, key string
	run        func()
}

type topMenu struct {
	title string
	items []menuItem
}

// menus описывает структуру меню. Горячие буквы (первая буква заголовка)
// уникальны: Ф, В, П, С — их ловит Alt+буква.
func (u *ui) menus() []topMenu {
	view := []menuItem{
		{"Панель чатов", "^B", u.toggleTree},
		{"Информация", "Alt+I", u.toggleInfo},
	}
	for i := range themes { // пункты выбора темы; активная помечена ✓
		t := themes[i]
		label := t.Name
		key := ""
		if t.Name == theme.Name {
			key = "✓"
		}
		view = append(view, menuItem{label, key, func() { u.setTheme(t) }})
	}
	return []topMenu{
		{"Файл", []menuItem{
			{"Справка", "F1", u.showHelp},
			{"О программе", "", u.showAbout},
			{"Выход", "Alt+X", u.app.Stop},
		}},
		{"Вид", view},
		{"Правка", []menuItem{
			{"Копировать", "c", u.copyMsg},
			{"Цитировать", "r", u.quoteMsg},
			{"Удалить", "d", u.deleteMsg},
			{"Выделить", "Spc", u.toggleSelect},
			{"Копировать выбранные", "y", u.copySelected},
			{"Открыть вложение", "o", u.openAttachment},
		}},
		{"Справка", []menuItem{
			{"Горячие клавиши", "F1", u.showHelp},
			{"О программе", "", u.showAbout},
		}},
	}
}

// renderMenuBar рисует текст меню-бара и запоминает X-координаты пунктов
// (u.menuX) — по ним позиционируются выпадающие списки.
func (u *ui) renderMenuBar() {
	u.menuX = u.menuX[:0]
	var b strings.Builder
	col := 2
	b.WriteString("  ")
	for i, m := range u.menus() {
		u.menuX = append(u.menuX, col)
		r := []rune(m.title)
		// регион t{i}: пробел, акцентная горячая буква, остаток, пробел
		fmt.Fprintf(&b, `["t%d"] [%s::b]%s[%s::-]%s [""]  `,
			i, theme.BarAccel, string(r[0]), theme.BarFg, string(r[1:]))
		col += 1 + len(r) + 1 + 2 // пробел + заголовок + пробел + 2 разделителя
	}
	u.header.SetText(b.String())
}

// openMenu показывает выпадающий список пункта i. Идемпотентно: при ← → старое
// выпадение заменяется новым. Список — отдельная страница с resize=false:
// рисуется только в своей области, интерфейс под ней остаётся виден.
//
// ВАЖНО про deadlock: app.SetFocus вызывает Blur старого фокуса ПОД мьютексом
// приложения. Поэтому здесь нет SetBlurFunc, который бы дёргал SetFocus, а
// закрытие меню идёт только из обработчиков ввода/мыши и Focus-колбэка панелей —
// они выполняются без мьютекса.
func (u *ui) openMenu(i int) {
	ms := u.menus()
	if i < 0 || i >= len(ms) {
		return
	}
	m := ms[i]
	u.menuActive = i
	u.setBarHighlight(fmt.Sprintf("t%d", i)) // визуальная подсветка пункта бара

	// Ширина выпадения = самая длинная строка «label … key».
	width := 0
	for _, it := range m.items {
		if w := len([]rune(it.label)) + len([]rune(it.key)); w > width {
			width = w
		}
	}
	width += 3 // зазор между подписью и клавишей

	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true).SetBorderColor(tcell.GetColor(theme.BorderActive))
	list.SetHighlightFullLine(true).SetWrapAround(true)
	list.SetMainTextColor(tcell.GetColor(theme.MenuText))
	list.SetSelectedTextColor(tcell.GetColor(theme.MenuSelFg))
	list.SetSelectedBackgroundColor(tcell.GetColor(theme.MenuSelBg))
	for _, it := range m.items {
		list.AddItem(menuItemLine(it, width), "", 0, nil)
	}
	list.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
		u.closeMenu()
		if idx >= 0 && idx < len(m.items) && m.items[idx].run != nil {
			m.items[idx].run()
		}
	})
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			u.closeMenu()
			return nil
		case tcell.KeyLeft:
			u.openMenu((i - 1 + len(ms)) % len(ms))
			return nil
		case tcell.KeyRight:
			u.openMenu((i + 1) % len(ms))
			return nil
		case tcell.KeyRune:
			// акселератор: первая буква пункта выполняет его
			if r := unicode.ToLower(ev.Rune()); r != ' ' {
				for k, it := range m.items {
					if lab := []rune(it.label); len(lab) > 0 && unicode.ToLower(lab[0]) == r {
						u.closeMenu()
						if m.items[k].run != nil {
							m.items[k].run()
						}
						return nil
					}
				}
			}
		}
		return ev
	})

	x := 0
	if i < len(u.menuX) {
		x = u.menuX[i]
	}
	list.SetRect(x, 1, width+2, len(m.items)+2) // +2 на рамку

	u.pages.RemovePage("menu")
	u.pages.AddPage("menu", shadowed{list}, false, true)
	u.app.SetFocus(list)
}

// menuItemLine форматирует строку пункта: подпись слева, клавиша справа,
// первая буква (акселератор) — отдельным цветом.
func menuItemLine(it menuItem, width int) string {
	line := it.label
	if it.key != "" {
		pad := width - len([]rune(it.label)) - len([]rune(it.key))
		if pad < 1 {
			pad = 1
		}
		line = it.label + strings.Repeat(" ", pad) + it.key
	}
	r := []rune(line)
	if len(r) == 0 {
		return line
	}
	return fmt.Sprintf("[%s::b]%s[-:-:-]%s", theme.MenuAccel, string(r[0]), string(r[1:]))
}

// setBarHighlight меняет визуальную подсветку меню-бара, не вызывая реакцию
// SetHighlightedFunc (guard от рекурсивного открытия).
func (u *ui) setBarHighlight(ids ...string) {
	u.menuGuard = true
	u.header.Highlight(ids...)
	u.menuGuard = false
}

// dismissMenu убирает выпадение и гасит подсветку бара, НЕ трогая фокус.
// Безопасно звать из Focus-колбэка панели (клик мимо / Tab).
func (u *ui) dismissMenu() {
	u.menuActive = -1
	u.pages.RemovePage("menu")
	u.setBarHighlight()
}

// dismissMenuIfOpen — закрыть меню, если открыто (вызов из Focus-колбэка панели).
func (u *ui) dismissMenuIfOpen() {
	if u.menuActive >= 0 {
		u.dismissMenu()
	}
}

// openContextMenu открывает локальное меню активной панели (Shift+F10).
func (u *ui) openContextMenu() {
	if u.app.GetFocus() == u.messages {
		u.openMenu(2) // Правка — действия над сообщениями
	} else {
		u.openMenu(1) // Вид
	}
}

// closeMenu закрывает меню и возвращает фокус последней активной панели.
// Вызывать только из обработчиков ввода/мыши (без мьютекса приложения).
func (u *ui) closeMenu() {
	u.dismissMenu()
	if u.lastPanel != nil {
		u.app.SetFocus(u.lastPanel)
	} else {
		u.app.SetFocus(u.tree)
	}
}

// runStatusAction выполняет действие по клику на подсказку статус-строки.
func (u *ui) runStatusAction(id string) {
	switch id {
	case "help":
		u.showHelp()
	case "menu":
		u.openMenu(0)
	case "tab":
		u.cycleFocus()
	case "tree":
		u.toggleTree()
	case "details":
		u.toggleInfo()
	case "send":
		text := strings.TrimSpace(u.input.GetText())
		if text != "" && u.open != nil && u.open.CanSend {
			u.submitMessage(text)
		}
	case "theme":
		u.cycleTheme()
	case "quit":
		u.app.Stop()
	case "copy":
		u.copyMsg()
	case "quote":
		u.quoteMsg()
	case "del":
		u.deleteMsg()
	case "mark":
		u.toggleSelect()
	case "copysel":
		u.copySelected()
	case "open":
		u.openAttachment()
	}
}

// showAbout показывает окно «О программе».
func (u *ui) showAbout() {
	text := "tgcli " + u.version + "\n" +
		"\n" +
		"Терминальный клиент Telegram\n" +
		"в стиле Turbo Pascal.\n" +
		"\n" +
		"MTProto через gotd/td."
	u.showDialog("О программе", text, []string{"OK"}, nil)
}

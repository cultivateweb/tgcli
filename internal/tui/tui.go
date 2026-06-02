// Package tui реализует интерактивный интерфейс tgcli на tview.
//
// Раскладка: сверху строка с именем/версией, по центру — аккордеон чатов слева
// (сгруппированы по типам), переписка и поле ввода в середине, панель деталей
// справа (прячется), статус снизу. Навигация, прокрутка и фокус — средствами
// tview; ширину символов считает tcell, что избавляет от перекоса рамок.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/cultivateweb/tgcli/internal/cache"
	"github.com/cultivateweb/tgcli/internal/telegram"
)

// Категории аккордеона в порядке отображения.
var kindOrder = []struct{ key, label string }{
	{"self", "★ Избранное"},
	{"user", "Люди"},
	{"bot", "Боты"},
	{"group", "Группы"},
	{"channel", "Каналы"},
}

// Цвета по типам чатов (выбор пользователя).
var kindColor = map[string]string{
	"self":    "#7dcfff",
	"user":    "#9ece6a",
	"bot":     "#e0af68",
	"group":   "#7aa2f7",
	"channel": "#bb9af7",
}

func groupKey(d telegram.Dialog) string {
	if d.Ref.Type == "self" {
		return "self"
	}
	switch d.Kind {
	case "bot", "group", "channel":
		return d.Kind
	default:
		return "user"
	}
}

type ui struct {
	ctx     context.Context
	sess    *telegram.Session
	cache   *cache.Cache
	version string

	app      *tview.Application
	pages    *tview.Pages
	tree     *tview.TreeView
	messages *tview.TextView
	input    *tview.TextArea
	details  *tview.List
	status   *tview.TextView
	mid      *tview.Flex

	detailValues []string // значения элементов панели «Детали» (для копирования)

	dialogs     []telegram.Dialog
	open        *telegram.Dialog
	history     []telegram.HistoryMessage
	showDetails bool

	msgSel   int          // индекс выбранного сообщения
	selected map[int]bool // мультивыбор сообщений
}

// Run строит и запускает интерфейс. c и updates могут быть nil.
func Run(ctx context.Context, sess *telegram.Session, c *cache.Cache, updates <-chan telegram.NewMessage, version string) error {
	roundBorders()
	u := &ui{ctx: ctx, sess: sess, cache: c, version: version,
		app: tview.NewApplication(), selected: map[int]bool{}}
	u.build()
	u.pages = tview.NewPages().AddPage("main", u.root(), true, true)

	saved := telegram.Dialog{Title: "Saved Messages", Kind: "user", Ref: telegram.PeerRef{Type: "self"}}
	saved.Peer = saved.Ref.InputPeer()
	u.openChat(saved)

	go u.loadDialogs()
	if updates != nil {
		go u.listenUpdates(updates)
	}

	return u.app.SetRoot(u.pages, true).EnableMouse(true).Run()
}

// roundBorders включает скруглённые углы у всех рамок.
func roundBorders() {
	tview.Borders.TopLeft = '╭'
	tview.Borders.TopRight = '╮'
	tview.Borders.BottomLeft = '╰'
	tview.Borders.BottomRight = '╯'
}

func (u *ui) build() {
	hex := func(s string) tcell.Color { return tcell.GetColor(s) }

	u.tree = tview.NewTreeView()
	u.tree.SetBorder(true).SetTitle(" Чаты ").SetBorderColor(hex("#3b4261"))
	u.tree.SetGraphics(true)
	u.tree.SetSelectedFunc(func(node *tview.TreeNode) {
		if ref := node.GetReference(); ref != nil {
			u.openChat(*ref.(*telegram.Dialog))
			u.app.SetFocus(u.input)
			return
		}
		node.SetExpanded(!node.IsExpanded())
	})

	u.messages = tview.NewTextView().SetDynamicColors(true).SetScrollable(true).SetWrap(true)
	u.messages.SetRegions(true)
	u.messages.SetBorder(true).SetTitle(" Сообщения ").SetBorderColor(hex("#3b4261"))
	u.messages.SetFocusFunc(func() { u.status.SetText(msgHints()) })
	u.messages.SetBlurFunc(func() { u.status.SetText(statusHints()) })
	u.messages.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyUp:
			u.selectMsg(u.msgSel - 1)
			return nil
		case tcell.KeyDown:
			u.selectMsg(u.msgSel + 1)
			return nil
		case tcell.KeyHome:
			u.selectMsg(0)
			return nil
		case tcell.KeyEnd:
			u.selectMsg(len(u.history) - 1)
			return nil
		}
		switch ev.Rune() {
		case 'c':
			u.copyMsg()
			return nil
		case 'r':
			u.quoteMsg()
			return nil
		case 'd':
			u.deleteMsg()
			return nil
		case ' ':
			u.toggleSelect()
			return nil
		case 'y':
			u.copySelected()
			return nil
		}
		return ev
	})

	u.input = tview.NewTextArea()
	u.input.SetPlaceholder("Сообщение…  (Enter — отправить, Alt+Enter — перенос строки)")
	u.input.SetBorder(true).SetTitle(" Ввод ").SetBorderColor(hex("#3b4261"))
	u.input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEnter && ev.Modifiers()&tcell.ModAlt == 0 {
			text := strings.TrimSpace(u.input.GetText())
			if text != "" && u.open != nil {
				go u.sendMessage(*u.open, text)
			}
			return nil
		}
		return ev
	})

	u.details = tview.NewList().ShowSecondaryText(true)
	u.details.SetBorder(true).SetTitle(" Детали ").SetBorderColor(hex("#3b4261"))
	u.details.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Rune() == 'c' {
			i := u.details.GetCurrentItem()
			if i >= 0 && i < len(u.detailValues) {
				if err := clipboard.WriteAll(u.detailValues[i]); err != nil {
					u.status.SetText("[#f7768e]Буфер недоступен (нужен xclip/xsel/wl-clipboard)[-]")
				} else {
					u.status.SetText("[#9ece6a]Скопировано[-]  " + statusHints())
				}
			}
			return nil
		}
		return ev
	})

	u.status = tview.NewTextView().SetDynamicColors(true)
	u.status.SetText(statusHints())

	// Заголовок окна терминала отражает открытый чат.
	u.app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		title := "tgcli"
		if u.open != nil {
			title = "tgcli — " + u.open.Title
		}
		s.SetTitle(title)
		return false
	})

	// Глобальные хоткеи.
	u.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyCtrlC:
			u.app.Stop()
			return nil
		case tcell.KeyTab:
			u.cycleFocus()
			return nil
		case tcell.KeyCtrlE:
			u.toggleDetails()
			return nil
		}
		return ev
	})
}

func (u *ui) root() *tview.Flex {
	center := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.messages, 0, 1, false).
		AddItem(u.input, 5, 0, false)

	u.mid = tview.NewFlex().
		AddItem(u.tree, 32, 0, true).
		AddItem(center, 0, 1, false)

	header := tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf("[#7dcfff::b]tgcli[-:-:-] [::d]%s[-:-:-]  [::d]— Telegram[-:-:-]", u.version))

	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(u.mid, 0, 1, false).
		AddItem(u.status, 1, 0, false)
}

// ── Фокус и панели ─────────────────────────────────────────────────────────

func (u *ui) cycleFocus() {
	order := []tview.Primitive{u.tree, u.messages, u.input}
	if u.showDetails {
		order = append(order, u.details)
	}
	cur := u.app.GetFocus()
	for i, p := range order {
		if p == cur {
			u.app.SetFocus(order[(i+1)%len(order)])
			return
		}
	}
	u.app.SetFocus(u.tree)
}

func (u *ui) toggleDetails() {
	u.showDetails = !u.showDetails
	if u.showDetails {
		u.renderDetails()
		u.mid.AddItem(u.details, 34, 0, false)
	} else {
		u.mid.RemoveItem(u.details)
		if u.app.GetFocus() == u.details {
			u.app.SetFocus(u.tree)
		}
	}
}

// ── Данные ─────────────────────────────────────────────────────────────────

func (u *ui) loadDialogs() {
	if u.cache != nil {
		if d, err := u.cache.Dialogs(); err == nil && len(d) > 0 {
			u.app.QueueUpdateDraw(func() { u.setDialogs(d) })
		}
	}
	d, err := u.sess.Dialogs(u.ctx, 200, false)
	if err != nil {
		u.app.QueueUpdateDraw(func() { u.status.SetText("[#f7768e]Ошибка диалогов: " + tview.Escape(err.Error())) })
		return
	}
	if u.cache != nil {
		_ = u.cache.SaveDialogs(d)
	}
	u.app.QueueUpdateDraw(func() { u.setDialogs(d) })
}

func (u *ui) setDialogs(d []telegram.Dialog) {
	u.dialogs = d
	u.buildTree()
}

func (u *ui) buildTree() {
	groups := map[string][]telegram.Dialog{}
	for _, d := range u.dialogs {
		k := groupKey(d)
		groups[k] = append(groups[k], d)
	}
	root := tview.NewTreeNode("").SetSelectable(false)
	for _, g := range kindOrder {
		list := groups[g.key]
		if len(list) == 0 {
			continue
		}
		col := tcell.GetColor(kindColor[g.key])
		cat := tview.NewTreeNode(fmt.Sprintf("%s (%d)", g.label, len(list))).
			SetColor(col).SetSelectable(true).SetExpanded(g.key == "self")
		for i := range list {
			dd := list[i]
			label := dd.Title
			if dd.Unread > 0 {
				label += fmt.Sprintf("  (%d)", dd.Unread)
			}
			cat.AddChild(tview.NewTreeNode(label).SetReference(&dd).SetColor(col))
		}
		root.AddChild(cat)
	}
	u.tree.SetRoot(root).SetCurrentNode(root)
}

func (u *ui) openChat(d telegram.Dialog) {
	dd := d
	u.open = &dd
	u.history = nil
	u.msgSel = -1
	u.selected = map[int]bool{}
	u.messages.SetTitle(" " + tview.Escape(dd.Title) + " ")
	u.messages.SetText("[#565f89]Загрузка истории…[-]")
	if u.showDetails {
		u.renderDetails()
	}

	go func() {
		key := dd.Ref.Key()
		if u.cache != nil {
			if h, err := u.cache.History(key); err == nil && len(h) > 0 {
				u.app.QueueUpdateDraw(func() {
					if u.open != nil && u.open.Ref.Key() == key {
						u.history = h
						u.renderMessages()
					}
				})
			}
		}
		h, err := u.sess.HistoryByPeer(u.ctx, dd.Peer, 60)
		if err == nil && u.cache != nil {
			_ = u.cache.SaveHistory(key, h)
		}
		u.app.QueueUpdateDraw(func() {
			if u.open == nil || u.open.Ref.Key() != key {
				return
			}
			if err != nil {
				u.messages.SetText("[#f7768e]Ошибка: " + tview.Escape(err.Error()))
				return
			}
			u.history = h
			u.renderMessages()
		})
	}()
}

func (u *ui) sendMessage(d telegram.Dialog, text string) {
	_, err := u.sess.SendToPeer(u.ctx, d.Peer, text)
	u.app.QueueUpdateDraw(func() {
		if err != nil {
			u.status.SetText("[#f7768e]Ошибка отправки: " + tview.Escape(err.Error()))
			return
		}
		u.input.SetText("", false)
		u.status.SetText(statusHints())
	})
	if err == nil {
		go func() {
			h, herr := u.sess.HistoryByPeer(u.ctx, d.Peer, 60)
			if herr == nil {
				if u.cache != nil {
					_ = u.cache.SaveHistory(d.Ref.Key(), h)
				}
				u.app.QueueUpdateDraw(func() {
					if u.open != nil && u.open.Ref.Key() == d.Ref.Key() {
						u.history = h
						u.renderMessages()
					}
				})
			}
		}()
	}
}

func (u *ui) listenUpdates(updates <-chan telegram.NewMessage) {
	for nm := range updates {
		nm := nm
		u.app.QueueUpdateDraw(func() {
			if u.open != nil && nm.PeerKey == u.open.Ref.Key() {
				u.history = append(u.history, nm.Message)
				u.renderMessages()
			} else if !nm.Message.Out {
				u.status.SetText("[#f7768e::b]● новое[-:-:-]  " + tview.Escape(statusHints()))
			}
		})
	}
}

// ── Рендер ─────────────────────────────────────────────────────────────────

func (u *ui) renderMessages() {
	var b strings.Builder
	for i, msg := range u.history {
		text := msg.Text
		if text == "" {
			text = "[вложение]"
		}
		b.WriteString(`["m` + fmt.Sprint(i) + `"]`) // регион сообщения
		if u.selected[i] {
			b.WriteString("[#e0af68]✓ [-]")
		}
		if msg.Out {
			fmt.Fprintf(&b, "[#9ece6a]→[-] %s", tview.Escape(text))
		} else {
			fmt.Fprintf(&b, "[#7dcfff::b]%s:[-:-:-] %s", tview.Escape(msg.Author), tview.Escape(text))
		}
		b.WriteString(`[""]` + "\n")
	}
	u.messages.SetText(b.String())
	if len(u.history) > 0 {
		if u.msgSel < 0 || u.msgSel >= len(u.history) {
			u.msgSel = len(u.history) - 1
		}
		u.messages.Highlight("m" + fmt.Sprint(u.msgSel))
		u.messages.ScrollToHighlight()
	}
}

func (u *ui) renderDetails() {
	u.details.Clear()
	u.detailValues = nil
	if u.open == nil {
		return
	}
	add := func(label, value string) {
		u.details.AddItem(label, value, 0, nil)
		u.detailValues = append(u.detailValues, value)
	}
	add("Имя", u.open.Title)
	add("Тип", u.open.Kind)
	add("ID", u.open.Ref.Key())
	if u.open.Unread > 0 {
		add("Непрочитано", fmt.Sprint(u.open.Unread))
	}
	u.details.SetTitle(" Детали — c копировать ")
}

// ── Действия над сообщениями ───────────────────────────────────────────────

func (u *ui) selectMsg(i int) {
	if len(u.history) == 0 {
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(u.history) {
		i = len(u.history) - 1
	}
	u.msgSel = i
	u.messages.Highlight("m" + fmt.Sprint(i))
	u.messages.ScrollToHighlight()
}

func (u *ui) selectedIndices() []int {
	var idx []int
	for i := 0; i < len(u.history); i++ {
		if u.selected[i] {
			idx = append(idx, i)
		}
	}
	return idx
}

func (u *ui) copyMsg() {
	if u.msgSel < 0 || u.msgSel >= len(u.history) {
		return
	}
	if err := clipboard.WriteAll(u.history[u.msgSel].Text); err != nil {
		u.status.SetText("[#f7768e]Буфер недоступен (нужен xclip/xsel/wl-clipboard)[-]")
		return
	}
	u.status.SetText("[#9ece6a]Скопировано[-]  " + msgHints())
}

func (u *ui) copySelected() {
	idx := u.selectedIndices()
	if len(idx) == 0 {
		u.copyMsg()
		return
	}
	var parts []string
	for _, i := range idx {
		parts = append(parts, u.history[i].Text)
	}
	if err := clipboard.WriteAll(strings.Join(parts, "\n\n")); err != nil {
		u.status.SetText("[#f7768e]Буфер недоступен (нужен xclip/xsel/wl-clipboard)[-]")
		return
	}
	u.status.SetText(fmt.Sprintf("[#9ece6a]Скопировано сообщений: %d[-]  %s", len(idx), msgHints()))
}

func (u *ui) quoteMsg() {
	if u.msgSel < 0 || u.msgSel >= len(u.history) {
		return
	}
	var b strings.Builder
	if cur := u.input.GetText(); cur != "" {
		b.WriteString(cur)
		if !strings.HasSuffix(cur, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, line := range strings.Split(u.history[u.msgSel].Text, "\n") {
		b.WriteString("> " + line + "\n")
	}
	u.input.SetText(b.String(), true)
	u.app.SetFocus(u.input)
}

func (u *ui) toggleSelect() {
	if u.msgSel < 0 || u.msgSel >= len(u.history) {
		return
	}
	if u.selected[u.msgSel] {
		delete(u.selected, u.msgSel)
	} else {
		u.selected[u.msgSel] = true
	}
	u.renderMessages()
}

func (u *ui) deleteMsg() {
	idx := u.selectedIndices()
	if len(idx) == 0 && u.msgSel >= 0 && u.msgSel < len(u.history) {
		idx = []int{u.msgSel}
	}
	if len(idx) == 0 || u.open == nil {
		return
	}
	ids := make([]int, 0, len(idx))
	for _, i := range idx {
		ids = append(ids, int(u.history[i].ID))
	}
	open := *u.open
	u.confirm(fmt.Sprintf("Удалить сообщений: %d? Удаление у всех участников, необратимо.", len(ids)), func() {
		go func() {
			err := u.sess.DeleteMessages(u.ctx, open.Peer, ids)
			u.app.QueueUpdateDraw(func() {
				if err != nil {
					u.status.SetText("[#f7768e]Ошибка удаления: " + tview.Escape(err.Error()) + "[-]")
					return
				}
				u.selected = map[int]bool{}
				u.status.SetText("[#9ece6a]Удалено[-]  " + msgHints())
			})
			go u.reloadHistory(open)
		}()
	})
}

func (u *ui) reloadHistory(d telegram.Dialog) {
	h, err := u.sess.HistoryByPeer(u.ctx, d.Peer, 60)
	if err != nil {
		return
	}
	if u.cache != nil {
		_ = u.cache.SaveHistory(d.Ref.Key(), h)
	}
	u.app.QueueUpdateDraw(func() {
		if u.open != nil && u.open.Ref.Key() == d.Ref.Key() {
			u.history = h
			u.renderMessages()
		}
	})
}

// confirm показывает модальное окно подтверждения поверх интерфейса.
func (u *ui) confirm(text string, onYes func()) {
	modal := tview.NewModal().SetText(text).
		AddButtons([]string{"Отмена", "Да"}).
		SetDoneFunc(func(_ int, label string) {
			u.pages.RemovePage("confirm")
			u.app.SetFocus(u.messages)
			if label == "Да" {
				onYes()
			}
		})
	u.pages.AddPage("confirm", modal, true, true)
}

func statusHints() string {
	return "[#565f89]Tab — фокус • Ctrl+E — детали • Enter — отправить • Ctrl+C — выход[-]"
}

func msgHints() string {
	return "[#565f89]↑↓ сообщения • c копир • r цитата • d удалить • Space выбор • y копир.выбранные[-]"
}

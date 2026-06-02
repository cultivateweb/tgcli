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
	tree     *tview.TreeView
	messages *tview.TextView
	input    *tview.TextArea
	details  *tview.TextView
	status   *tview.TextView
	mid      *tview.Flex

	dialogs     []telegram.Dialog
	open        *telegram.Dialog
	history     []telegram.HistoryMessage
	showDetails bool
}

// Run строит и запускает интерфейс. c и updates могут быть nil.
func Run(ctx context.Context, sess *telegram.Session, c *cache.Cache, updates <-chan telegram.NewMessage, version string) error {
	roundBorders()
	u := &ui{ctx: ctx, sess: sess, cache: c, version: version, app: tview.NewApplication()}
	u.build()

	saved := telegram.Dialog{Title: "Saved Messages", Kind: "user", Ref: telegram.PeerRef{Type: "self"}}
	saved.Peer = saved.Ref.InputPeer()
	u.openChat(saved)

	go u.loadDialogs()
	if updates != nil {
		go u.listenUpdates(updates)
	}

	return u.app.SetRoot(u.root(), true).EnableMouse(true).Run()
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
	u.messages.SetBorder(true).SetTitle(" Сообщения ").SetBorderColor(hex("#3b4261"))

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

	u.details = tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	u.details.SetBorder(true).SetTitle(" Детали ").SetBorderColor(hex("#3b4261"))

	u.status = tview.NewTextView().SetDynamicColors(true)
	u.status.SetText(statusHints())

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
	order := []tview.Primitive{u.tree, u.input}
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
	for _, msg := range u.history {
		text := msg.Text
		if text == "" {
			text = "[вложение]"
		}
		if msg.Out {
			fmt.Fprintf(&b, "[#9ece6a]→[-] %s\n", tview.Escape(text))
		} else {
			fmt.Fprintf(&b, "[#7dcfff::b]%s:[-:-:-] %s\n", tview.Escape(msg.Author), tview.Escape(text))
		}
	}
	u.messages.SetText(b.String())
	u.messages.ScrollToEnd()
}

func (u *ui) renderDetails() {
	if u.open == nil {
		u.details.SetText("")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[::b]%s[-:-:-]\n\n", tview.Escape(u.open.Title))
	fmt.Fprintf(&b, "[#565f89]Тип:[-]  %s\n", u.open.Kind)
	fmt.Fprintf(&b, "[#565f89]ID:[-]   %s\n", u.open.Ref.Key())
	u.details.SetText(b.String())
}

func statusHints() string {
	return "[#565f89]Tab — фокус • Ctrl+E — детали • Enter — отправить • Ctrl+C — выход[-]"
}

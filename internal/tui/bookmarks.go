package tui

// Локальные закладки раздела «★ Избранное»: добавление (Ctrl+D с вводом имени),
// удаление (d + подтверждение), хранение в конфиге. В Telegram не уходят.

import (
	"fmt"

	"github.com/rivo/tview"

	"github.com/cultivateweb/tgcli/internal/config"
	"github.com/cultivateweb/tgcli/internal/telegram"
)

// bookmarkDialog восстанавливает Dialog из закладки для отображения и открытия.
// Если такой чат есть в загруженном списке — подтягивает из него свежие
// непрочитанные/право писать/форум, оставляя пользовательское имя закладки.
func (u *ui) bookmarkDialog(b config.Bookmark) telegram.Dialog {
	d := telegram.Dialog{
		Title:   b.Name,
		Kind:    b.Kind,
		CanSend: b.CanSend,
		Ref:     telegram.PeerRef{Type: b.PeerType, ID: b.PeerID, AccessHash: b.AccessHash},
	}
	d.Peer = d.Ref.InputPeer()
	if live := u.dialogByKey(d.Ref.Key()); live != nil {
		d.Unread = live.Unread
		d.CanSend = live.CanSend
		d.Forum = live.Forum
		d.Peer = live.Peer
		if d.Kind == "" {
			d.Kind = live.Kind
		}
	}
	return d
}

// dialogByKey ищет загруженный диалог по ключу чата (nil, если не найден).
func (u *ui) dialogByKey(key string) *telegram.Dialog {
	for i := range u.dialogs {
		if u.dialogs[i].Ref.Key() == key {
			return &u.dialogs[i]
		}
	}
	return nil
}

// isBookmarked сообщает, есть ли чат с таким ключом в избранном.
func (u *ui) isBookmarked(key string) bool {
	if u.cfg == nil {
		return false
	}
	for _, b := range u.cfg.Bookmarks {
		if (telegram.PeerRef{Type: b.PeerType, ID: b.PeerID, AccessHash: b.AccessHash}).Key() == key {
			return true
		}
	}
	return false
}

// bookmarkCurrent добавляет в избранное чат под курсором дерева, а если открыта
// переписка — её (Ctrl+D), спросив имя закладки (по умолчанию — название чата).
func (u *ui) bookmarkCurrent() {
	if u.cfg == nil {
		return
	}
	var d *telegram.Dialog
	if u.app.GetFocus() == u.tree {
		if node := u.tree.GetCurrentNode(); node != nil {
			if dd, ok := node.GetReference().(*telegram.Dialog); ok {
				d = dd // чат под курсором (не категория)
			}
		}
	} else if u.open != nil {
		d = u.open // открытая переписка
	}
	if d == nil {
		return
	}
	if u.isBookmarked(d.Ref.Key()) {
		u.status.SetText("[" + theme.Warn + "]Уже в избранном[-]")
		return
	}
	dd := *d
	u.showPrompt("Имя для избранного", d.Title, func(name string, ok bool) {
		if !ok {
			return
		}
		u.addBookmark(dd, name)
	})
}

// addBookmark сохраняет закладку в конфиг и пересобирает дерево.
func (u *ui) addBookmark(d telegram.Dialog, name string) {
	if u.cfg == nil {
		return
	}
	u.cfg.Bookmarks = append(u.cfg.Bookmarks, config.Bookmark{
		Name:       name,
		PeerType:   d.Ref.Type,
		PeerID:     d.Ref.ID,
		AccessHash: d.Ref.AccessHash,
		Kind:       d.Kind,
		CanSend:    d.CanSend,
	})
	u.saveConfig()
	u.buildTree()
	u.status.SetText("[" + theme.Success + "]Добавлено в избранное: " + name + "[-]")
}

// removeBookmarkNode убирает закладку из избранного (d на узле раздела
// «★ Избранное»), спросив подтверждение. На остальных узлах — ничего не делает.
func (u *ui) removeBookmarkNode(node *tview.TreeNode) {
	if node == nil || u.favNode == nil || u.treeParent(node) != u.favNode {
		return
	}
	d, ok := node.GetReference().(*telegram.Dialog)
	if !ok {
		return
	}
	key, name := d.Ref.Key(), d.Title
	u.confirm(fmt.Sprintf("Убрать «%s» из избранного?", name), func() {
		u.removeBookmark(key)
	})
}

// removeBookmark удаляет закладку по ключу чата, сохраняет конфиг и пересобирает дерево.
func (u *ui) removeBookmark(key string) {
	if u.cfg == nil {
		return
	}
	kept := u.cfg.Bookmarks[:0]
	for _, b := range u.cfg.Bookmarks {
		if (telegram.PeerRef{Type: b.PeerType, ID: b.PeerID, AccessHash: b.AccessHash}).Key() != key {
			kept = append(kept, b)
		}
	}
	u.cfg.Bookmarks = kept
	u.saveConfig()
	u.buildTree()
	u.status.SetText("[" + theme.Success + "]Убрано из избранного[-]")
}

// saveConfig пишет конфиг в фоне (не блокируя UI).
func (u *ui) saveConfig() {
	if u.cfg == nil {
		return
	}
	go func() { _ = u.cfg.Save() }()
}

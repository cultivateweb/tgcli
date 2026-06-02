package cache

import (
	"path/filepath"
	"testing"

	"github.com/gotd/td/tg"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func TestDialogsRoundTrip(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	in := []telegram.Dialog{
		{Title: "Алиса", Kind: "user", Unread: 3, Ref: telegram.PeerRef{Type: "user", ID: 42, AccessHash: 99}},
		{Title: "Избранное", Kind: "user", Ref: telegram.PeerRef{Type: "self"}},
	}
	if err := c.SaveDialogs(in); err != nil {
		t.Fatal(err)
	}

	out, err := c.Dialogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("ожидалось 2 диалога, получено %d", len(out))
	}
	if out[0].Title != "Алиса" || out[0].Unread != 3 {
		t.Errorf("диалог искажён: %+v", out[0])
	}
	// Peer должен восстановиться из Ref как InputPeerUser с тем же id/hash.
	u, ok := out[0].Peer.(*tg.InputPeerUser)
	if !ok || u.UserID != 42 || u.AccessHash != 99 {
		t.Errorf("peer не восстановлен: %#v", out[0].Peer)
	}
	if _, ok := out[1].Peer.(*tg.InputPeerSelf); !ok {
		t.Errorf("self-peer не восстановлен: %#v", out[1].Peer)
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	in := []telegram.HistoryMessage{{ID: 1, Author: "Вы", Out: true, Text: "привет"}}
	if err := c.SaveHistory("user:42", in); err != nil {
		t.Fatal(err)
	}
	out, err := c.History("user:42")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Text != "привет" {
		t.Errorf("история искажена: %+v", out)
	}
	// Несуществующий ключ — пусто, без ошибки.
	empty, err := c.History("missing")
	if err != nil || len(empty) != 0 {
		t.Errorf("ожидалась пустая история, получено %v (err=%v)", empty, err)
	}
}

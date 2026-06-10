package telegram

import (
	"testing"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

// fwdMsg собирает tg.Message с заданным заголовком пересылки.
func fwdMsg(h *tg.MessageFwdHeader) *tg.Message {
	m := &tg.Message{Message: "привет"}
	if h != nil {
		m.SetFwdFrom(*h)
	}
	return m
}

func TestForwardFromUser(t *testing.T) {
	ent := peer.NewEntities(
		map[int64]*tg.User{42: {ID: 42, AccessHash: 777, FirstName: "Аня"}},
		nil, nil,
	)
	h := &tg.MessageFwdHeader{}
	h.SetFromID(&tg.PeerUser{UserID: 42})

	fw := forwardFrom(fwdMsg(h), ent)
	if fw == nil {
		t.Fatal("ожидался Forward, получен nil")
	}
	if fw.Origin != "Аня" {
		t.Errorf("Origin = %q, ожидалось «Аня»", fw.Origin)
	}
	if fw.Kind != "user" {
		t.Errorf("Kind = %q, ожидалось user", fw.Kind)
	}
	if fw.From.Type != "user" || fw.From.ID != 42 || fw.From.AccessHash != 777 {
		t.Errorf("From = %+v, ожидался user/42/777", fw.From)
	}
}

// В Saved Messages сообщение, сохранённое из группы: автор в FromID, а чат-
// источник для навигации — в SavedFromPeer с SavedFromMsgID.
func TestForwardSavedFromGroup(t *testing.T) {
	ent := peer.NewEntities(
		map[int64]*tg.User{42: {ID: 42, FirstName: "Аня"}},
		nil,
		map[int64]*tg.Channel{9: {ID: 9, AccessHash: 555, Title: "Чат", Megagroup: true}},
	)
	h := &tg.MessageFwdHeader{}
	h.SetFromID(&tg.PeerUser{UserID: 42})
	h.SetSavedFromPeer(&tg.PeerChannel{ChannelID: 9})
	h.SetSavedFromMsgID(1234)

	fw := forwardFrom(fwdMsg(h), ent)
	if fw == nil {
		t.Fatal("ожидался Forward, получен nil")
	}
	// Origin остаётся автором, но навигация ведёт в чат-источник.
	if fw.Origin != "Аня" {
		t.Errorf("Origin = %q, ожидалось «Аня»", fw.Origin)
	}
	if fw.From.Type != "channel" || fw.From.ID != 9 || fw.From.AccessHash != 555 {
		t.Errorf("From = %+v, ожидался channel/9/555", fw.From)
	}
	if fw.MsgID != 1234 {
		t.Errorf("MsgID = %d, ожидалось 1234", fw.MsgID)
	}
}

// Скрытый отправитель: только FromName, ссылки на профиль нет — навигация недоступна.
func TestForwardHidden(t *testing.T) {
	h := &tg.MessageFwdHeader{}
	h.SetFromName("Некто")

	fw := forwardFrom(fwdMsg(h), peer.NewEntities(nil, nil, nil))
	if fw == nil {
		t.Fatal("ожидался Forward, получен nil")
	}
	if fw.Origin != "Некто" {
		t.Errorf("Origin = %q, ожидалось «Некто»", fw.Origin)
	}
	if fw.From.Type != "" {
		t.Errorf("From = %+v, ожидался пустой (навигация недоступна)", fw.From)
	}
}

func TestForwardNoneForPlainMessage(t *testing.T) {
	if fw := forwardFrom(fwdMsg(nil), peer.NewEntities(nil, nil, nil)); fw != nil {
		t.Errorf("у обычного сообщения Fwd должен быть nil, получен %+v", fw)
	}
}

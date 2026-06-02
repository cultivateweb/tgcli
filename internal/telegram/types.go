package telegram

import (
	"strconv"
	"time"

	"github.com/gotd/td/tg"
)

// Message — исходящее сообщение, которое нужно отправить.
type Message struct {
	To   string // получатель: @username, телефон или chat id
	Text string
}

// SentMessage — результат успешной отправки.
type SentMessage struct {
	ID   int64
	Date time.Time
}

// Dialog — строка списка диалогов (команда chats).
type Dialog struct {
	Title   string    `json:"title"`
	Kind    string    `json:"kind"` // user, bot, group, channel
	Unread  int       `json:"unread"`
	Date    time.Time `json:"date"`
	Preview string    `json:"preview"`
	CanSend bool      `json:"can_send"` // можно ли писать в этот чат
	Ref     PeerRef   `json:"peer"`     // сериализуемая ссылка на чат (для кеша)

	// Peer адресует чат для History/Send в TUI. Не сериализуется напрямую
	// (интерфейс); восстанавливается из Ref при чтении из кеша.
	Peer tg.InputPeerClass `json:"-"`
}

// NewMessage — сообщение из потока обновлений (live): к какому чату относится
// (PeerKey совпадает с PeerRef.Key()) и само сообщение.
type NewMessage struct {
	PeerKey string
	Message HistoryMessage
}

// PeerRef — компактная сериализуемая ссылка на собеседника/чат: тип, id и
// access_hash. Нужна, чтобы хранить диалоги в кеше и восстанавливать InputPeer.
type PeerRef struct {
	Type       string `json:"type"` // self, user, chat, channel
	ID         int64  `json:"id"`
	AccessHash int64  `json:"access_hash"`
}

// InputPeer восстанавливает InputPeer из ссылки.
func (r PeerRef) InputPeer() tg.InputPeerClass {
	switch r.Type {
	case "self":
		return &tg.InputPeerSelf{}
	case "user":
		return &tg.InputPeerUser{UserID: r.ID, AccessHash: r.AccessHash}
	case "chat":
		return &tg.InputPeerChat{ChatID: r.ID}
	case "channel":
		return &tg.InputPeerChannel{ChannelID: r.ID, AccessHash: r.AccessHash}
	}
	return &tg.InputPeerEmpty{}
}

// Key — стабильный ключ чата для кеша истории.
func (r PeerRef) Key() string {
	return r.Type + ":" + strconv.FormatInt(r.ID, 10)
}

// peerRefFrom извлекает ссылку из InputPeer.
func peerRefFrom(p tg.InputPeerClass) PeerRef {
	switch v := p.(type) {
	case *tg.InputPeerSelf:
		return PeerRef{Type: "self"}
	case *tg.InputPeerUser:
		return PeerRef{Type: "user", ID: v.UserID, AccessHash: v.AccessHash}
	case *tg.InputPeerChat:
		return PeerRef{Type: "chat", ID: v.ChatID}
	case *tg.InputPeerChannel:
		return PeerRef{Type: "channel", ID: v.ChannelID, AccessHash: v.AccessHash}
	}
	return PeerRef{}
}

// HistoryMessage — сообщение из истории чата (команда read).
type HistoryMessage struct {
	ID     int64     `json:"id"`
	Date   time.Time `json:"date"`
	Author string    `json:"author"`
	Out    bool      `json:"out"` // исходящее (отправлено мной)
	Text   string    `json:"text"`
}

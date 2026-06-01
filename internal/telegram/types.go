package telegram

import (
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

	// Peer адресует чат для History/Send в TUI (не сериализуется).
	Peer tg.InputPeerClass `json:"-"`
}

// HistoryMessage — сообщение из истории чата (команда read).
type HistoryMessage struct {
	ID     int64     `json:"id"`
	Date   time.Time `json:"date"`
	Author string    `json:"author"`
	Out    bool      `json:"out"` // исходящее (отправлено мной)
	Text   string    `json:"text"`
}

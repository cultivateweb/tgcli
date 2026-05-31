package telegram

import "time"

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

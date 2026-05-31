package telegram

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
)

// resolveTarget превращает строку получателя в билдер сообщения.
//
// Особый случай «me»/«self»/«saved» — Избранное (Saved Messages).
// Остальное передаётся в Resolve: поддерживаются @username, username,
// t.me/username и подобные формы (см. message.Sender.Resolve).
func resolveTarget(sender *message.Sender, to string) *message.RequestBuilder {
	switch strings.ToLower(strings.TrimSpace(to)) {
	case "me", "self", "saved":
		return sender.Self()
	default:
		return sender.Resolve(to)
	}
}

// sentFromUpdates извлекает id и дату созданного сообщения из ответа сервера.
// Делается best-effort: если структуру разобрать не удалось, возвращается
// результат с текущим временем и нулевым id.
func sentFromUpdates(upd tg.UpdatesClass) SentMessage {
	sent := SentMessage{Date: time.Now()}

	switch u := upd.(type) {
	case *tg.Updates:
		for _, up := range u.Updates {
			if id, ok := messageID(up); ok {
				sent.ID = id
			}
		}
	case *tg.UpdateShort:
		if id, ok := messageID(u.Update); ok {
			sent.ID = id
		}
	}
	return sent
}

func messageID(u tg.UpdateClass) (int64, bool) {
	switch up := u.(type) {
	case *tg.UpdateMessageID:
		return int64(up.ID), true
	case *tg.UpdateNewMessage:
		if m, ok := up.Message.(*tg.Message); ok {
			return int64(m.ID), true
		}
	}
	return 0, false
}

// DisplayName формирует читаемое имя пользователя: «Имя Фамилия (@username)».
// Безопасна к nil.
func DisplayName(u *tg.User) string {
	if u == nil {
		return "неизвестный пользователь"
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	switch {
	case name != "" && u.Username != "":
		return name + " (@" + u.Username + ")"
	case u.Username != "":
		return "@" + u.Username
	case name != "":
		return name
	default:
		return "id " + strconv.FormatInt(u.ID, 10)
	}
}

// removeSession удаляет файл сессии; отсутствие файла ошибкой не считается.
func removeSession(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

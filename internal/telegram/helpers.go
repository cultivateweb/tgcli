package telegram

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/query/messages"
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

// dialogFromElem превращает элемент диалога в строку Dialog.
func dialogFromElem(e dialogs.Elem) Dialog {
	var d Dialog
	d.Peer = e.Peer
	d.Ref = peerRefFrom(e.Peer)
	if dlg, ok := e.Dialog.(*tg.Dialog); ok {
		d.Unread = dlg.UnreadCount
	}
	d.Title, d.Kind = peerTitleKind(e.Peer, e.Entities)
	d.Title = sanitize(d.Title)
	// «Saved Messages» приходит как обычный пользователь (свой же аккаунт) —
	// узнаём его по флагу Self и относим к Избранному.
	if v, ok := e.Peer.(*tg.InputPeerUser); ok {
		if usr, ok := e.Entities.User(v.UserID); ok && usr.Self {
			d.Kind = "self"
			d.Title = "Saved Messages"
		}
	}
	d.CanSend = canSend(e.Peer, e.Entities)
	d.Mine = isCreator(e.Peer, e.Entities)
	if v, ok := e.Peer.(*tg.InputPeerChannel); ok {
		if ch, ok := e.Entities.Channel(v.ChannelID); ok {
			d.Forum = ch.Forum
		}
	}
	if msg, ok := e.Last.(*tg.Message); ok {
		d.Date = time.Unix(int64(msg.Date), 0)
		d.Preview = oneLine(msg.Message)
	}
	return d
}

// historyFromElem превращает элемент истории в HistoryMessage.
func historyFromElem(e messages.Elem) HistoryMessage {
	var hm HistoryMessage
	msg, ok := e.Msg.(*tg.Message)
	if !ok {
		return hm
	}
	hm.ID = int64(msg.ID)
	hm.Date = time.Unix(int64(msg.Date), 0)
	hm.Out = msg.Out
	hm.Text = sanitize(msg.Message)
	hm.Spans = spansFrom(msg.Message, msg.Entities)
	hm.Media = mediaFrom(msg)
	hm.Author = sanitize(messageAuthor(msg, e.Entities, e.Peer))
	return hm
}

// canSend определяет, может ли пользователь писать в чат. Для broadcast-каналов
// писать может только владелец или админ с правом постинга; для супергрупп —
// если отправка не запрещена по умолчанию. Личка, боты, обычные группы и
// Избранное — всегда можно. При нехватке данных разрешаем (не блокируем зря).
func canSend(p tg.InputPeerClass, ent peer.Entities) bool {
	v, ok := p.(*tg.InputPeerChannel)
	if !ok {
		return true
	}
	ch, ok := ent.Channel(v.ChannelID)
	if !ok {
		return true
	}
	if ch.Creator {
		return true
	}
	if ch.Broadcast {
		if r, ok := ch.GetAdminRights(); ok {
			return r.PostMessages
		}
		return false
	}
	if r, ok := ch.GetDefaultBannedRights(); ok && r.SendMessages {
		return false
	}
	return true
}

// isCreator сообщает, является ли пользователь создателем группы/канала.
// Для лички и ботов понятия владельца нет — возвращает false.
func isCreator(p tg.InputPeerClass, ent peer.Entities) bool {
	switch v := p.(type) {
	case *tg.InputPeerChat:
		if ch, ok := ent.Chat(v.ChatID); ok {
			return ch.Creator
		}
	case *tg.InputPeerChannel:
		if ch, ok := ent.Channel(v.ChannelID); ok {
			return ch.Creator
		}
	}
	return false
}

// peerTitleKind возвращает отображаемое имя и тип собеседника/чата.
func peerTitleKind(p tg.InputPeerClass, ent peer.Entities) (title, kind string) {
	switch v := p.(type) {
	case *tg.InputPeerSelf:
		return "Избранное", "user"
	case *tg.InputPeerUser:
		if u, ok := ent.User(v.UserID); ok {
			if u.Bot {
				return DisplayName(u), "bot"
			}
			return DisplayName(u), "user"
		}
		return "id " + strconv.FormatInt(v.UserID, 10), "user"
	case *tg.InputPeerChat:
		if ch, ok := ent.Chat(v.ChatID); ok {
			return ch.Title, "group"
		}
		return "id " + strconv.FormatInt(v.ChatID, 10), "group"
	case *tg.InputPeerChannel:
		if ch, ok := ent.Channel(v.ChannelID); ok {
			if ch.Megagroup {
				return ch.Title, "supergroup"
			}
			return ch.Title, "channel"
		}
		return "id " + strconv.FormatInt(v.ChannelID, 10), "channel"
	}
	return "—", "?"
}

// messageAuthor определяет автора сообщения для отображения.
func messageAuthor(m *tg.Message, ent peer.Entities, p tg.InputPeerClass) string {
	if m.Out {
		return "Вы"
	}
	if from, ok := m.GetFromID(); ok {
		if pu, ok := from.(*tg.PeerUser); ok {
			if u, ok := ent.User(pu.UserID); ok {
				return DisplayName(u)
			}
			return "id " + strconv.FormatInt(pu.UserID, 10)
		}
	}
	title, _ := peerTitleKind(p, ent)
	return title
}

// oneLine схлопывает текст в одну строку, чистит и обрезает для превью.
func oneLine(s string) string {
	s = sanitize(strings.ReplaceAll(s, "\n", " "))
	if r := []rune(s); len(r) > 60 {
		return string(r[:57]) + "…"
	}
	return s
}

// sanitize удаляет невидимые и управляющие символы, из-за которых ширина строки
// в TUI считается не так, как её рисует терминал (перекос рамок): zero-width,
// bidi-метки, вариативные селекторы, управляющие символы. Переносы строк и
// табуляции схлопываются в пробел, результат обрезается по краям (для имён,
// превью, однострочного текста).
func sanitize(s string) string {
	return strings.TrimSpace(cleanRunes(s, false))
}

// cleanInline чистит те же невидимые символы, что и sanitize, но сохраняет
// обычные пробелы и переносы строк и не обрезает края — для сегментов
// форматированного текста (Span), где важна разметка пробелов и абзацев.
func cleanInline(s string) string {
	return cleanRunes(s, true)
}

// cleanRunes — общий фильтр невидимых/управляющих символов. Если keepBreaks,
// переносы строк сохраняются (табуляция → пробел), иначе всё схлопывается в пробел.
func cleanRunes(s string, keepBreaks bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			if keepBreaks {
				b.WriteRune('\n')
			} else {
				b.WriteRune(' ')
			}
		case r == '\t' || r == '\r':
			b.WriteRune(' ')
		case unicode.IsControl(r):
			// прочие управляющие — пропускаем
		case r == 0xFEFF || r == 0x2060: // BOM, word-joiner
		case r >= 0x200B && r <= 0x200F: // zero-width, LRM, RLM
		case r >= 0x202A && r <= 0x202E: // bidi embedding/override
		case r >= 0x2066 && r <= 0x2069: // bidi isolates
		case r >= 0xFE00 && r <= 0xFE0F: // вариативные селекторы
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// newMessageFrom преобразует сообщение из обновления в NewMessage для подписчиков.
func newMessageFrom(msg tg.MessageClass, ent tg.Entities) (NewMessage, bool) {
	m, ok := msg.(*tg.Message)
	if !ok {
		return NewMessage{}, false
	}
	key := peerKey(m.PeerID)
	if key == "" {
		return NewMessage{}, false
	}
	hm := HistoryMessage{
		ID:     int64(m.ID),
		Date:   time.Unix(int64(m.Date), 0),
		Out:    m.Out,
		Text:   sanitize(m.Message),
		Spans:  spansFrom(m.Message, m.Entities),
		Media:  mediaFrom(m),
		Author: sanitize(liveAuthor(m, ent)),
	}
	return NewMessage{PeerKey: key, Message: hm}, true
}

// liveAuthor определяет автора входящего сообщения из обновления.
func liveAuthor(m *tg.Message, ent tg.Entities) string {
	if m.Out {
		return "Вы"
	}
	if from, ok := m.GetFromID(); ok {
		if pu, ok := from.(*tg.PeerUser); ok {
			if u, ok := ent.Users[pu.UserID]; ok {
				return DisplayName(u)
			}
		}
	}
	// Для личной переписки автор — собеседник (peer сообщения).
	if pu, ok := m.PeerID.(*tg.PeerUser); ok {
		if u, ok := ent.Users[pu.UserID]; ok {
			return DisplayName(u)
		}
	}
	return "—"
}

// peerKey строит ключ чата (совпадает с PeerRef.Key()) из Peer обновления.
func peerKey(p tg.PeerClass) string {
	switch v := p.(type) {
	case *tg.PeerUser:
		return "user:" + strconv.FormatInt(v.UserID, 10)
	case *tg.PeerChat:
		return "chat:" + strconv.FormatInt(v.ChatID, 10)
	case *tg.PeerChannel:
		return "channel:" + strconv.FormatInt(v.ChannelID, 10)
	}
	return ""
}

// removeSession удаляет файл сессии; отсутствие файла ошибкой не считается.
func removeSession(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

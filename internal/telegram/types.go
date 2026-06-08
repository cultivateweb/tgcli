package telegram

import (
	"strconv"
	"strings"
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

	// ReadOutboxMaxID — максимальный ID исходящего сообщения, прочитанного
	// собеседником. Сообщение с ID ≤ этого значения считается прочитанным
	// (статус «✓✓ прочитано»), большее — просто отправленным («✓»).
	ReadOutboxMaxID int64 `json:"read_outbox_max_id,omitempty"`

	CanSend bool    `json:"can_send"` // можно ли писать в этот чат
	Mine    bool    `json:"mine"`     // я создатель (для групп/каналов)
	Muted   bool    `json:"muted"`    // уведомления выключены (в муте)
	Forum   bool    `json:"forum"`    // супергруппа-форум (есть темы)
	Ref     PeerRef `json:"peer"`     // сериализуемая ссылка на чат (для кеша)

	// TopicID/TopicTitle непусты, когда диалог адресует тему форума, а не сам
	// чат: история и отправка идут через тред темы.
	TopicID    int    `json:"-"`
	TopicTitle string `json:"-"`

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

// MsgStatus — статус доставки исходящего сообщения (для входящих — StatusNone).
type MsgStatus string

const (
	StatusNone    MsgStatus = ""        // входящее — статус не показываем
	StatusSending MsgStatus = "sending" // отправляется (локальное эхо без серверного ID)
	StatusError   MsgStatus = "error"   // ошибка отправки
	StatusSent    MsgStatus = "sent"    // принято сервером, ещё не прочитано (✓)
	StatusRead    MsgStatus = "read"    // прочитано собеседником (✓✓)
)

// HistoryMessage — сообщение из истории чата (команда read).
type HistoryMessage struct {
	ID     int64     `json:"id"`
	Date   time.Time `json:"date"`
	Author string    `json:"author"`
	Out    bool      `json:"out"`  // исходящее (отправлено мной)
	Text   string    `json:"text"` // plain-текст (для копирования/цитаты/превью)

	// Spans — текст, разбитый на форматированные сегменты (из entities Telegram).
	// Пусто, если форматирования нет; тогда для вывода берётся Text.
	Spans []Span `json:"spans,omitempty"`
	// Media — описание вложения (фото/файл/видео…), nil если вложения нет.
	Media *Media `json:"media,omitempty"`

	// Status — статус доставки исходящего сообщения. Не сериализуется: read-state
	// меняется со временем, поэтому вычисляется заново в TUI из ReadOutboxMaxID.
	Status MsgStatus `json:"-"`
}

// Plain возвращает текст сообщения без разметки для копирования/цитаты:
// собирается из Spans (сохраняя переносы строк), иначе берётся Text.
func (m HistoryMessage) Plain() string {
	if len(m.Spans) == 0 {
		return m.Text
	}
	var b strings.Builder
	for _, s := range m.Spans {
		b.WriteString(s.Text)
	}
	return strings.TrimSpace(b.String())
}

// Span — сегмент текста с единым форматированием. Несколько флагов могут
// сочетаться (например, B && I). URL непуст для ссылок.
type Span struct {
	Text string `json:"t"`
	B    bool   `json:"b,omitempty"` // bold
	I    bool   `json:"i,omitempty"` // italic
	U    bool   `json:"u,omitempty"` // underline
	S    bool   `json:"s,omitempty"` // strikethrough
	Code bool   `json:"c,omitempty"` // моноширинный (code/pre)
	URL  string `json:"url,omitempty"`
}

// Media — описание вложения. Достаточно для отображения; для скачивания
// сообщение перезапрашивается заново (DownloadMedia), чтобы file_reference был
// свежим.
type Media struct {
	Kind     string `json:"kind"` // photo, video, audio, voice, gif, sticker, document
	FileName string `json:"file_name,omitempty"`
	MIME     string `json:"mime,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// Label — человекочитаемое описание вложения для строки в переписке.
func (m *Media) Label() string {
	if m == nil {
		return ""
	}
	name := m.FileName
	if name == "" {
		name = mediaKindName(m.Kind)
	}
	if m.Size > 0 {
		return name + " (" + humanSize(m.Size) + ")"
	}
	return name
}

func mediaKindName(kind string) string {
	switch kind {
	case "photo":
		return "фото"
	case "video":
		return "видео"
	case "round":
		return "кружок"
	case "gif":
		return "GIF"
	case "audio":
		return "аудио"
	case "voice":
		return "голосовое"
	case "sticker":
		return "стикер"
	default:
		return "файл"
	}
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " Б"
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + []string{"КБ", "МБ", "ГБ", "ТБ"}[exp]
}

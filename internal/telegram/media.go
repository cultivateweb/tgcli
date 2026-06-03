package telegram

// Форматирование сообщений (entities → Span) и вложения (Media + скачивание).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

// spansFrom разбивает текст на форматированные сегменты по entities Telegram.
// Смещения entities заданы в единицах UTF-16, поэтому считаем по utf16-коду.
// Возвращает как минимум один сегмент, если текст непуст (даже без форматирования).
func spansFrom(text string, entities []tg.MessageEntityClass) []Span {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	units := utf16.Encode([]rune(text))
	n := len(units)

	type style struct {
		b, i, u, s, code bool
		url              string
	}
	styles := make([]style, n)
	apply := func(off, length int, f func(*style)) {
		if off < 0 {
			length += off
			off = 0
		}
		end := off + length
		if end > n {
			end = n
		}
		for k := off; k < end; k++ {
			f(&styles[k])
		}
	}
	rangeText := func(off, length int) string {
		if off < 0 {
			length += off
			off = 0
		}
		end := off + length
		if end > n {
			end = n
		}
		if off >= end {
			return ""
		}
		return string(utf16.Decode(units[off:end]))
	}

	for _, e := range entities {
		off, length := e.GetOffset(), e.GetLength()
		switch ent := e.(type) {
		case *tg.MessageEntityBold:
			apply(off, length, func(s *style) { s.b = true })
		case *tg.MessageEntityItalic:
			apply(off, length, func(s *style) { s.i = true })
		case *tg.MessageEntityUnderline:
			apply(off, length, func(s *style) { s.u = true })
		case *tg.MessageEntityStrike:
			apply(off, length, func(s *style) { s.s = true })
		case *tg.MessageEntityCode:
			apply(off, length, func(s *style) { s.code = true })
		case *tg.MessageEntityPre:
			apply(off, length, func(s *style) { s.code = true })
		case *tg.MessageEntityTextURL:
			apply(off, length, func(s *style) { s.url = ent.URL })
		case *tg.MessageEntityURL:
			link := rangeText(off, length)
			apply(off, length, func(s *style) { s.url = link })
		case *tg.MessageEntityEmail:
			link := "mailto:" + rangeText(off, length)
			apply(off, length, func(s *style) { s.url = link })
		}
	}

	var spans []Span
	flush := func(from, to int, st style) {
		t := cleanInline(string(utf16.Decode(units[from:to])))
		if t == "" {
			return
		}
		spans = append(spans, Span{Text: t, B: st.b, I: st.i, U: st.u, S: st.s, Code: st.code, URL: st.url})
	}
	start := 0
	for k := 1; k <= n; k++ {
		if k == n || styles[k] != styles[start] {
			flush(start, k, styles[start])
			start = k
		}
	}
	return spans
}

// mediaFrom извлекает описание вложения из сообщения (nil, если вложения нет
// либо тип не поддерживается для открытия — гео, контакт, опрос и т.п.).
func mediaFrom(m *tg.Message) *Media {
	media, ok := m.GetMedia()
	if !ok {
		return nil
	}
	switch v := media.(type) {
	case *tg.MessageMediaPhoto:
		if _, ok := v.Photo.(*tg.Photo); !ok {
			return nil
		}
		return &Media{Kind: "photo"}
	case *tg.MessageMediaDocument:
		doc, ok := v.Document.(*tg.Document)
		if !ok {
			return nil
		}
		return &Media{Kind: docKind(doc), MIME: doc.MimeType, Size: doc.Size, FileName: docFileName(doc)}
	}
	return nil
}

// docKind определяет вид документа по его атрибутам.
func docKind(doc *tg.Document) string {
	var animated, video, sticker bool
	var audioVoice, audio bool
	for _, a := range doc.Attributes {
		switch at := a.(type) {
		case *tg.DocumentAttributeAnimated:
			animated = true
		case *tg.DocumentAttributeVideo:
			video = true
		case *tg.DocumentAttributeSticker:
			sticker = true
		case *tg.DocumentAttributeAudio:
			if at.Voice {
				audioVoice = true
			} else {
				audio = true
			}
		}
	}
	switch {
	case sticker:
		return "sticker"
	case animated:
		return "gif"
	case video:
		return "video"
	case audioVoice:
		return "voice"
	case audio:
		return "audio"
	default:
		return "document"
	}
}

func docFileName(doc *tg.Document) string {
	for _, a := range doc.Attributes {
		if fn, ok := a.(*tg.DocumentAttributeFilename); ok {
			return sanitize(fn.FileName)
		}
	}
	return ""
}

// DownloadMedia перезапрашивает сообщение (ради свежего file_reference) и
// скачивает вложение в каталог dir. Возвращает путь к сохранённому файлу.
func (s *Session) DownloadMedia(ctx context.Context, peer tg.InputPeerClass, msgID int64, dir string) (string, error) {
	m, err := s.fetchMessage(ctx, peer, msgID)
	if err != nil {
		return "", err
	}
	media, ok := m.GetMedia()
	if !ok {
		return "", errors.New("в сообщении нет вложения")
	}
	loc, name, err := fileLocation(media, msgID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if _, err := downloader.NewDownloader().Download(s.api, loc).ToPath(ctx, path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Session) fetchMessage(ctx context.Context, peer tg.InputPeerClass, msgID int64) (*tg.Message, error) {
	ids := []tg.InputMessageClass{&tg.InputMessageID{ID: int(msgID)}}
	var (
		res tg.MessagesMessagesClass
		err error
	)
	if ch, ok := inputChannel(peer); ok {
		res, err = s.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: ch, ID: ids})
	} else {
		res, err = s.api.MessagesGetMessages(ctx, ids)
	}
	if err != nil {
		return nil, err
	}
	for _, mc := range messagesSlice(res) {
		if m, ok := mc.(*tg.Message); ok && int64(m.ID) == msgID {
			return m, nil
		}
	}
	return nil, errors.New("сообщение не найдено на сервере")
}

func messagesSlice(res tg.MessagesMessagesClass) []tg.MessageClass {
	switch v := res.(type) {
	case *tg.MessagesMessages:
		return v.Messages
	case *tg.MessagesMessagesSlice:
		return v.Messages
	case *tg.MessagesChannelMessages:
		return v.Messages
	}
	return nil
}

func inputChannel(peer tg.InputPeerClass) (tg.InputChannelClass, bool) {
	if v, ok := peer.(*tg.InputPeerChannel); ok {
		return &tg.InputChannel{ChannelID: v.ChannelID, AccessHash: v.AccessHash}, true
	}
	return nil, false
}

// fileLocation строит location для скачивания и подсказывает имя файла.
func fileLocation(media tg.MessageMediaClass, msgID int64) (tg.InputFileLocationClass, string, error) {
	switch v := media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := v.Photo.(*tg.Photo)
		if !ok {
			return nil, "", errors.New("фото недоступно")
		}
		loc := &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     largestPhotoSize(photo),
		}
		return loc, fmt.Sprintf("photo_%d.jpg", msgID), nil
	case *tg.MessageMediaDocument:
		doc, ok := v.Document.(*tg.Document)
		if !ok {
			return nil, "", errors.New("документ недоступен")
		}
		name := docFileName(doc)
		if name == "" {
			name = fmt.Sprintf("file_%d%s", msgID, extFromMIME(doc.MimeType))
		}
		return doc.AsInputDocumentFileLocation(), name, nil
	}
	return nil, "", errors.New("этот тип вложения нельзя открыть")
}

// largestPhotoSize выбирает тип самого крупного варианта фото.
func largestPhotoSize(photo *tg.Photo) string {
	best, bestArea := "", -1
	for _, sz := range photo.Sizes {
		switch s := sz.(type) {
		case *tg.PhotoSize:
			if area := s.W * s.H; area > bestArea {
				best, bestArea = s.Type, area
			}
		case *tg.PhotoSizeProgressive:
			if area := s.W * s.H; area > bestArea {
				best, bestArea = s.Type, area
			}
		default:
			if best == "" {
				best = sz.GetType()
			}
		}
	}
	return best
}

func extFromMIME(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "application/pdf":
		return ".pdf"
	}
	if i := strings.IndexByte(mime, '/'); i >= 0 && i+1 < len(mime) {
		return "." + mime[i+1:]
	}
	return ""
}

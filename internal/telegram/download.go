package telegram

// Скачивание вложений с докачкой, прогрессом и паузой. Качаем чанками через
// upload.getFile со смещением (высокоуровневый downloader смещение не отдаёт).
// Файл пишется в path+".part" и переименовывается в path по завершении — это и
// признак «скачано целиком», и точка докачки (смещение = размер .part).

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

const downloadChunk = 512 * 1024 // кратно 4096 и ≤ 1 МБ — требование API

// DownloadMediaTo скачивает вложение сообщения в path, докачивая с уже
// сохранённого смещения. onProgress (может быть nil) вызывается с done/total
// байт (total=0, если размер неизвестен). Отмена через ctx сохраняет .part.
func (s *Session) DownloadMediaTo(ctx context.Context, peer tg.InputPeerClass, msgID int64, path string, onProgress func(done, total int64)) error {
	m, err := s.fetchMessage(ctx, peer, msgID)
	if err != nil {
		return err
	}
	media, ok := m.GetMedia()
	if !ok {
		return errors.New("в сообщении нет вложения")
	}
	loc, _, err := fileLocation(media, msgID)
	if err != nil {
		return err
	}
	total := mediaSize(media)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	part := path + ".part"

	// Смещение докачки — размер .part, выровненный вниз по 4096 (требование API).
	var offset int64
	if fi, statErr := os.Stat(part); statErr == nil {
		offset = fi.Size() - fi.Size()%4096
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := f.Truncate(offset); err != nil { // отбросить недописанный хвост
		f.Close()
		return err
	}
	if _, err := f.Seek(offset, 0); err != nil {
		f.Close()
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return err
		}
		res, err := s.api.UploadGetFile(ctx, &tg.UploadGetFileRequest{
			Location: loc, Offset: offset, Limit: downloadChunk,
		})
		if err != nil {
			f.Close()
			return err
		}
		uf, ok := res.(*tg.UploadFile)
		if !ok { // CDN-редирект — докачка не поддержана, качаем целиком
			f.Close()
			_ = os.Remove(part)
			if _, derr := downloader.NewDownloader().Download(s.api, loc).ToPath(ctx, path); derr != nil {
				return derr
			}
			if onProgress != nil && total > 0 {
				onProgress(total, total)
			}
			return nil
		}
		n := len(uf.Bytes)
		if n > 0 {
			if _, err := f.Write(uf.Bytes); err != nil {
				f.Close()
				return err
			}
			offset += int64(n)
			if onProgress != nil {
				onProgress(offset, total)
			}
		}
		if n < downloadChunk {
			break
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(part, path)
}

func mediaSize(media tg.MessageMediaClass) int64 {
	if d, ok := media.(*tg.MessageMediaDocument); ok {
		if doc, ok := d.Document.(*tg.Document); ok {
			return doc.Size
		}
	}
	return 0
}

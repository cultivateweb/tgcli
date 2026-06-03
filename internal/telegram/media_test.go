package telegram

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestSpansFrom(t *testing.T) {
	// "Привет мир" — кириллица по 1 UTF-16-единице; жирным «мир» (offset 7, len 3).
	text := "Привет мир"
	spans := spansFrom(text, []tg.MessageEntityClass{
		&tg.MessageEntityBold{Offset: 7, Length: 3},
	})
	if len(spans) != 2 {
		t.Fatalf("ожидалось 2 сегмента, получено %d: %+v", len(spans), spans)
	}
	if spans[0].Text != "Привет " || spans[0].B {
		t.Errorf("сегмент 0 неверен: %+v", spans[0])
	}
	if spans[1].Text != "мир" || !spans[1].B {
		t.Errorf("сегмент 1 должен быть жирным «мир»: %+v", spans[1])
	}

	// Наложение bold+italic и ссылка.
	spans = spansFrom("link", []tg.MessageEntityClass{
		&tg.MessageEntityTextURL{Offset: 0, Length: 4, URL: "https://e.com"},
		&tg.MessageEntityBold{Offset: 0, Length: 2},
	})
	if len(spans) != 2 {
		t.Fatalf("ожидалось 2 сегмента, получено %d: %+v", len(spans), spans)
	}
	if !spans[0].B || spans[0].URL != "https://e.com" {
		t.Errorf("сегмент 0 — жирная ссылка: %+v", spans[0])
	}
	if spans[1].B || spans[1].URL != "https://e.com" {
		t.Errorf("сегмент 1 — нежирная ссылка: %+v", spans[1])
	}

	// Эмодзи занимает 2 UTF-16-единицы: bold после него (offset 2).
	spans = spansFrom("😀ab", []tg.MessageEntityClass{
		&tg.MessageEntityBold{Offset: 2, Length: 2},
	})
	if len(spans) != 2 || spans[0].Text != "😀" || spans[1].Text != "ab" || !spans[1].B {
		t.Errorf("разбор эмодзи неверен: %+v", spans)
	}

	if spansFrom("", nil) != nil {
		t.Error("пустой текст должен давать nil")
	}
}

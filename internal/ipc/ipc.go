// Package ipc описывает протокол общения тонких клиентов (events, status) с
// демоном-резидентом через unix-сокет и даёт клиентскую сторону этого протокола.
//
// Формат — построчный JSON (одна JSON-запись на строку). Клиент посылает один
// Request, сервер отвечает потоком Envelope: для events — бесконечным потоком
// сообщений, для status — одним кадром. Никаких внешних зависимостей: только
// стандартная библиотека, чтобы протокол оставался простым и стабильным.
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Имена команд, которые клиент шлёт демону в поле Request.Cmd.
const (
	CmdEvents = "events" // подписаться на поток входящих сообщений
	CmdStatus = "status" // запросить состояние демона (один ответ)
	CmdStop   = "stop"   // попросить демона корректно завершиться
)

// Типы кадров, которые демон шлёт клиенту в поле Envelope.Type.
const (
	TypeMessage = "message" // входящее сообщение (для events)
	TypeStatus  = "status"  // ответ на CmdStatus
	TypeOK      = "ok"      // подтверждение (например, на CmdStop)
	TypeError   = "error"   // ошибка обработки команды
)

// Request — команда клиента демону. Передаётся первой строкой соединения.
type Request struct {
	Cmd string `json:"cmd"`
}

// Envelope — кадр ответа демона. Заполнено ровно одно из полей по Type.
type Envelope struct {
	Type    string        `json:"type"`
	Error   string        `json:"error,omitempty"`
	Status  *StatusReply  `json:"status,omitempty"`
	Message *MessageEvent `json:"message,omitempty"`
}

// StatusReply — состояние демона для команды status.
type StatusReply struct {
	Self        string    `json:"self"`         // под кем залогинен демон
	StartedAt   time.Time `json:"started_at"`   // момент запуска (для аптайма)
	Subscribers int       `json:"subscribers"`  // сколько клиентов слушают events
}

// MessageEvent — входящее сообщение в потоке events. Это «тонкий» срез
// telegram.NewMessage: только то, что нужно показать в стриме/уведомлении.
type MessageEvent struct {
	PeerKey string    `json:"peer_key"`        // ключ чата (PeerRef.Key)
	From    string    `json:"from"`            // автор сообщения
	Text    string    `json:"text"`            // текст (plain)
	Media   string    `json:"media,omitempty"` // описание вложения, если есть
	Out     bool      `json:"out"`             // исходящее (отправлено мной)
	Time    time.Time `json:"time"`            // время сообщения
}

// Open подключается к сокету демона, отправляет команду cmd и возвращает
// соединение вместе с декодером ответов. Вызывающий обязан закрыть conn.
// Если демон не слушает сокет, возвращается ошибка с понятным текстом.
func Open(socketPath, cmd string) (net.Conn, *json.Decoder, error) {
	conn, err := net.DialTimeout("unix", socketPath, 3*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("демон не запущен (нет ответа на %s): выполните «tgcli daemon»", socketPath)
	}
	if err := json.NewEncoder(conn).Encode(Request{Cmd: cmd}); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("отправка команды демону: %w", err)
	}
	return conn, json.NewDecoder(bufio.NewReader(conn)), nil
}

// Status запрашивает состояние демона одним кадром.
func Status(socketPath string) (*StatusReply, error) {
	conn, dec, err := Open(socketPath, CmdStatus)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var env Envelope
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("чтение ответа демона: %w", err)
	}
	if env.Type == TypeError {
		return nil, fmt.Errorf("демон: %s", env.Error)
	}
	if env.Status == nil {
		return nil, fmt.Errorf("демон: неожиданный ответ %q", env.Type)
	}
	return env.Status, nil
}

// Stop просит демона корректно завершиться и ждёт подтверждения.
func Stop(socketPath string) error {
	conn, dec, err := Open(socketPath, CmdStop)
	if err != nil {
		return err
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var env Envelope
	if err := dec.Decode(&env); err != nil {
		// Демон мог закрыть соединение, уже начав останавливаться — это не ошибка.
		return nil
	}
	if env.Type == TypeError {
		return fmt.Errorf("демон: %s", env.Error)
	}
	return nil
}

// Running сообщает, отвечает ли демон на сокете (быстрая проверка для status).
func Running(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Subscribe подписывается на поток событий и вызывает onMessage для каждого
// входящего, пока ctx жив или соединение не закрыто. Закрывает conn при ctx.Done.
func Subscribe(ctx context.Context, socketPath string, onMessage func(MessageEvent)) error {
	conn, dec, err := Open(socketPath, CmdEvents)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Закрываем соединение при отмене контекста, чтобы разблокировать Decode.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	for {
		var env Envelope
		if err := dec.Decode(&env); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		switch env.Type {
		case TypeMessage:
			if env.Message != nil {
				onMessage(*env.Message)
			}
		case TypeError:
			return fmt.Errorf("демон: %s", env.Error)
		}
	}
}

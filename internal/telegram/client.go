// Package telegram инкапсулирует работу с Telegram по MTProto
// (через github.com/gotd/td) от имени пользовательского аккаунта.
//
// Клиент создаётся на каждую операцию заново: gotd-клиент живёт внутри
// client.Run(ctx, ...) — соединение поднимается, выполняется работа и
// корректно закрывается. Сессия сохраняется в файл (config.SessionPath),
// поэтому повторный вход не требуется.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"

	"github.com/cultivateweb/tgcli/internal/config"
)

// ErrNotAuthorized возвращается, когда операция требует входа, а сессии нет.
var ErrNotAuthorized = errors.New("требуется вход: запустите «tgcli auth»")

// Client — обёртка над MTProto-клиентом gotd.
type Client struct {
	cfg *config.Config
}

// New создаёт клиента на основе конфигурации.
func New(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

// newGotd собирает неподключённый gotd-клиент с файловым хранилищем сессии.
func (c *Client) newGotd() (*telegram.Client, error) {
	apiID, apiHash, err := c.cfg.Credentials()
	if err != nil {
		return nil, err
	}
	// gotd FileStorage пишет сессию голым os.WriteFile и каталог не создаёт.
	// Если каталога нет, сохранение сессии падает и роняет соединение gotd
	// («engine was closed»), а сессия не персистится между запусками.
	sessionPath := c.cfg.SessionPath()
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o700); err != nil {
		return nil, fmt.Errorf("создание каталога сессии: %w", err)
	}
	return telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: sessionPath},
	}), nil
}

// run поднимает соединение и выполняет fn внутри активной сессии.
func (c *Client) run(ctx context.Context, fn func(ctx context.Context, client *telegram.Client) error) error {
	client, err := c.newGotd()
	if err != nil {
		return err
	}
	return client.Run(ctx, func(ctx context.Context) error {
		return fn(ctx, client)
	})
}

// Login выполняет интерактивную авторизацию и сохраняет сессию в файл.
// Если сессия уже валидна, вход пропускается.
func (c *Client) Login(ctx context.Context) (*tg.User, error) {
	var self *tg.User
	err := c.run(ctx, func(ctx context.Context, client *telegram.Client) error {
		flow := auth.NewFlow(newTerminalAuth(c.cfg.PhoneNumber()), auth.SendCodeOptions{})
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return err
		}
		u, err := client.Self(ctx)
		if err != nil {
			return err
		}
		self = u
		return nil
	})
	return self, err
}

// Logout завершает сессию на сервере и удаляет локальный файл сессии.
func (c *Client) Logout(ctx context.Context) error {
	err := c.run(ctx, func(ctx context.Context, client *telegram.Client) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized {
			return nil // нечего завершать
		}
		_, err = client.API().AuthLogOut(ctx)
		return err
	})
	// Файл сессии удаляем в любом случае — чтобы локально не осталось доступа.
	if rmErr := removeSession(c.cfg.SessionPath()); rmErr != nil && err == nil {
		err = rmErr
	}
	return err
}

// Status сообщает, авторизован ли клиент, и кто текущий пользователь.
func (c *Client) Status(ctx context.Context) (authorized bool, self *tg.User, err error) {
	err = c.run(ctx, func(ctx context.Context, client *telegram.Client) error {
		st, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		authorized = st.Authorized
		self = st.User
		return nil
	})
	return authorized, self, err
}

// SendMessage отправляет текстовое сообщение и возвращает данные результата.
func (c *Client) SendMessage(ctx context.Context, m Message) (SentMessage, error) {
	var sent SentMessage
	err := c.run(ctx, func(ctx context.Context, client *telegram.Client) error {
		st, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !st.Authorized {
			return ErrNotAuthorized
		}

		sender := message.NewSender(client.API())
		builder := resolveTarget(sender, m.To)

		upd, err := builder.Text(ctx, m.Text)
		if err != nil {
			return fmt.Errorf("отправка %q: %w", m.To, err)
		}
		sent = sentFromUpdates(upd)
		return nil
	})
	return sent, err
}

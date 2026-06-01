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
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

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
		// Уже авторизованы — повторный вход не нужен.
		if st, err := client.Auth().Status(ctx); err == nil && st.Authorized {
			if st.User != nil {
				self = st.User
				return nil
			}
			self, err = client.Self(ctx)
			return err
		}

		u, err := signIn(ctx, client.Auth(), newPrompter(c.cfg.PhoneNumber()))
		if err != nil {
			return err
		}
		self = u
		return nil
	})
	return self, err
}

// signIn проводит интерактивный вход вручную (вместо высокоуровневого
// auth.Flow), чтобы поддержать повторную отправку кода: пользователь может
// запросить новый код, а при ответе сервера PHONE_CODE_EXPIRED код
// перезапрашивается автоматически.
func signIn(ctx context.Context, a *auth.Client, p *prompter) (*tg.User, error) {
	phone, err := p.Phone()
	if err != nil {
		return nil, err
	}

	code, err := sentCode(a.SendCode(ctx, phone, auth.SendCodeOptions{}))
	if err != nil {
		return nil, authError("отправка кода", err)
	}

	for {
		input, resend, err := p.Code(code)
		if err != nil {
			return nil, err
		}
		if resend {
			code, err = sentCode(a.ResendCode(ctx, phone, code.PhoneCodeHash))
			if err != nil {
				return nil, authError("повторная отправка кода", err)
			}
			fmt.Println("Код отправлен повторно.")
			continue
		}

		authz, err := a.SignIn(ctx, phone, input, code.PhoneCodeHash)
		switch {
		case errors.Is(err, auth.ErrPasswordAuthNeeded):
			pw, perr := p.Password()
			if perr != nil {
				return nil, perr
			}
			authz, err = a.Password(ctx, pw)
			if err != nil {
				return nil, authError("двухфакторная аутентификация", err)
			}
		case tgerr.Is(err, "PHONE_CODE_EXPIRED"):
			fmt.Println("Код истёк, запрашиваю новый…")
			code, err = sentCode(a.ResendCode(ctx, phone, code.PhoneCodeHash))
			if err != nil {
				return nil, authError("повторная отправка кода", err)
			}
			continue
		case tgerr.Is(err, "PHONE_CODE_INVALID"):
			fmt.Println("Неверный код. Попробуйте ещё раз.")
			continue
		case err != nil:
			return nil, authError("вход по коду", err)
		}

		return userFromAuth(authz)
	}
}

// authError возвращает понятное объяснение для известных ошибок авторизации,
// а для прочих — добавляет контекст к исходной ошибке.
func authError(context string, err error) error {
	if friendly := friendlyAuthError(err); friendly != err {
		return friendly
	}
	return fmt.Errorf("%s: %w", context, err)
}

// friendlyAuthError переводит типовые ошибки Telegram в понятные сообщения.
// Если ошибка не распознана — возвращает её без изменений.
func friendlyAuthError(err error) error {
	if err == nil {
		return nil
	}
	if d, ok := tgerr.AsFloodWait(err); ok {
		return fmt.Errorf("Telegram просит подождать перед следующей попыткой входа: %s. "+
			"Это анти-спам — не запускайте «auth» до истечения этого времени", d.Round(time.Second))
	}
	switch {
	case tgerr.Is(err, "SEND_CODE_UNAVAILABLE"):
		return errors.New("Telegram временно не отправляет код на этот номер — обычно из-за " +
			"частых запросов кода за короткое время. Это ограничение на стороне Telegram, " +
			"а не ошибка tgcli. Подождите несколько часов (иногда до суток) и попробуйте ОДИН раз, " +
			"не повторяя «auth» подряд")
	case tgerr.Is(err, "PHONE_NUMBER_INVALID"):
		return errors.New("неверный номер телефона: проверьте формат, например +380731234567")
	case tgerr.Is(err, "PHONE_NUMBER_BANNED"):
		return errors.New("этот номер заблокирован в Telegram")
	case tgerr.Is(err, "PHONE_NUMBER_UNOCCUPIED"):
		return errors.New("на этот номер ещё нет аккаунта Telegram: сначала зарегистрируйтесь в официальном клиенте")
	case tgerr.Is(err, "PHONE_PASSWORD_FLOOD"):
		return errors.New("слишком много попыток ввода пароля 2FA — подождите и попробуйте позже")
	}
	return err
}

// sentCode приводит ответ SendCode/ResendCode к *tg.AuthSentCode.
func sentCode(sent tg.AuthSentCodeClass, err error) (*tg.AuthSentCode, error) {
	if err != nil {
		return nil, err
	}
	code, ok := sent.(*tg.AuthSentCode)
	if !ok {
		return nil, fmt.Errorf("неожиданный ответ сервера на отправку кода: %T", sent)
	}
	return code, nil
}

// userFromAuth достаёт пользователя из ответа авторизации.
func userFromAuth(a *tg.AuthAuthorization) (*tg.User, error) {
	if a == nil {
		return nil, errors.New("пустой ответ авторизации")
	}
	u, ok := a.User.(*tg.User)
	if !ok {
		return nil, fmt.Errorf("неожиданный тип пользователя в ответе: %T", a.User)
	}
	return u, nil
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

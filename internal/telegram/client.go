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
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/mdp/qrterminal/v3"

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

// baseOptions готовит api-данные и базовые опции gotd-клиента (хранилище
// сессии). Каталог сессии создаётся заранее: gotd FileStorage пишет сессию
// голым os.WriteFile и каталог не создаёт — без него сохранение падает и
// роняет соединение («engine was closed»), а сессия не персистится.
func (c *Client) baseOptions() (apiID int, apiHash string, opts telegram.Options, err error) {
	apiID, apiHash, err = c.cfg.Credentials()
	if err != nil {
		return
	}
	sessionPath := c.cfg.SessionPath()
	if mkErr := os.MkdirAll(filepath.Dir(sessionPath), 0o700); mkErr != nil {
		err = fmt.Errorf("создание каталога сессии: %w", mkErr)
		return
	}
	opts = telegram.Options{SessionStorage: &session.FileStorage{Path: sessionPath}}
	return
}

// newGotd собирает неподключённый gotd-клиент с файловым хранилищем сессии.
func (c *Client) newGotd() (*telegram.Client, error) {
	apiID, apiHash, opts, err := c.baseOptions()
	if err != nil {
		return nil, err
	}
	return telegram.NewClient(apiID, apiHash, opts), nil
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

// LoginQR выполняет вход по QR-коду: пользователь сканирует код в уже
// авторизованном приложении (Настройки → Устройства → Подключить устройство).
// Не использует SMS/код из приложения, поэтому обходит ограничение
// SEND_CODE_UNAVAILABLE. Требует обработчик обновлений, чтобы поймать сигнал
// о подтверждении (tg.UpdateLoginToken).
func (c *Client) LoginQR(ctx context.Context) (*tg.User, error) {
	apiID, apiHash, opts, err := c.baseOptions()
	if err != nil {
		return nil, err
	}
	dispatcher := tg.NewUpdateDispatcher()
	opts.UpdateHandler = dispatcher
	client := telegram.NewClient(apiID, apiHash, opts)

	var self *tg.User
	err = client.Run(ctx, func(ctx context.Context) error {
		// Уже авторизованы — повторный вход не нужен.
		if st, err := client.Auth().Status(ctx); err == nil && st.Authorized {
			if st.User != nil {
				self = st.User
				return nil
			}
			self, err = client.Self(ctx)
			return err
		}

		loggedIn := qrlogin.OnLoginToken(dispatcher)
		authz, err := client.QR().Auth(ctx, loggedIn, func(ctx context.Context, token qrlogin.Token) error {
			showQR(token)
			return nil
		})
		// При включённой 2FA после сканирования нужен облачный пароль.
		// qrlogin.Import отдаёт сырую SESSION_PASSWORD_NEEDED (в отличие от
		// обычного SignIn, который конвертирует её в ErrPasswordAuthNeeded).
		if errors.Is(err, auth.ErrPasswordAuthNeeded) || tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
			fmt.Println("\nУ аккаунта включена двухфакторная аутентификация.")
			pw, perr := newPrompter("").Password()
			if perr != nil {
				return perr
			}
			pwAuthz, perr := client.Auth().Password(ctx, pw)
			if perr != nil {
				return authError("двухфакторная аутентификация", perr)
			}
			self, perr = userFromAuth(pwAuthz)
			return perr
		}
		if err != nil {
			return friendlyAuthError(err)
		}
		self, err = userFromAuth(authz)
		return err
	})
	return self, err
}

// showQR печатает QR-код входа и ссылку-резерв.
func showQR(token qrlogin.Token) {
	fmt.Println("\nОтсканируйте QR-код в Telegram на телефоне:")
	fmt.Println("Настройки → Устройства → Подключить устройство.")
	qrterminal.GenerateHalfBlock(token.URL(), qrterminal.L, os.Stdout)
	fmt.Printf("\nНе сканируется? Откройте ссылку на залогиненном устройстве:\n%s\n", token.URL())
	fmt.Println("Ожидаю подтверждения…")
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

// requireAuth возвращает ErrNotAuthorized, если сессия не авторизована.
func requireAuth(ctx context.Context, client *telegram.Client) error {
	st, err := client.Auth().Status(ctx)
	if err != nil {
		return err
	}
	if !st.Authorized {
		return ErrNotAuthorized
	}
	return nil
}

// Session — открытое соединение для серии операций. Методы работают поверх
// уже поднятого gotd-клиента, не открывая новых соединений. Используется TUI
// (одно живое соединение на всё время работы) и одиночными командами.
type Session struct {
	api *tg.Client
}

// API даёт доступ к низкоуровневому клиенту gotd.
func (s *Session) API() *tg.Client { return s.api }

// WithSession открывает соединение, проверяет авторизацию и выполняет fn,
// держа одну активную сессию всё время работы fn (подходит для TUI).
func (c *Client) WithSession(ctx context.Context, fn func(ctx context.Context, s *Session) error) error {
	return c.run(ctx, func(ctx context.Context, client *telegram.Client) error {
		if err := requireAuth(ctx, client); err != nil {
			return err
		}
		return fn(ctx, &Session{api: client.API()})
	})
}

// WithLiveSession — как WithSession, но клиент создаётся с обработчиком
// обновлений, и fn получает канал входящих сообщений (live). Канал закрывается
// при завершении работы.
func (c *Client) WithLiveSession(ctx context.Context, fn func(ctx context.Context, s *Session, updates <-chan NewMessage) error) error {
	apiID, apiHash, opts, err := c.baseOptions()
	if err != nil {
		return err
	}
	dispatcher := tg.NewUpdateDispatcher()
	updates := make(chan NewMessage, 128)
	push := func(msg tg.MessageClass, ent tg.Entities) {
		if nm, ok := newMessageFrom(msg, ent); ok {
			// Не блокируем обработчик обновлений, если подписчик не успевает.
			select {
			case updates <- nm:
			default:
			}
		}
	}
	dispatcher.OnNewMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		push(u.Message, e)
		return nil
	})
	dispatcher.OnNewChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		push(u.Message, e)
		return nil
	})
	opts.UpdateHandler = dispatcher

	client := telegram.NewClient(apiID, apiHash, opts)
	return client.Run(ctx, func(ctx context.Context) error {
		if err := requireAuth(ctx, client); err != nil {
			return err
		}
		defer close(updates)
		return fn(ctx, &Session{api: client.API()}, updates)
	})
}

// Send отправляет текстовое сообщение в чат to (@username/me/телефон).
func (s *Session) Send(ctx context.Context, to, text string) (SentMessage, error) {
	upd, err := resolveTarget(message.NewSender(s.api), to).Text(ctx, text)
	if err != nil {
		return SentMessage{}, fmt.Errorf("отправка %q: %w", to, err)
	}
	return sentFromUpdates(upd), nil
}

// DeleteMessages удаляет сообщения по id в чате peer (revoke — у всех участников).
func (s *Session) DeleteMessages(ctx context.Context, peer tg.InputPeerClass, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	if ch, ok := peer.(*tg.InputPeerChannel); ok {
		_, err := s.api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
			ID:      ids,
		})
		return err
	}
	_, err := s.api.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{
		Revoke: true,
		ID:     ids,
	})
	return err
}

// SendToPeer отправляет сообщение по готовому peer (для TUI, где чат выбран
// из списка диалогов и может не иметь @username).
func (s *Session) SendToPeer(ctx context.Context, peer tg.InputPeerClass, text string) (SentMessage, error) {
	upd, err := message.NewSender(s.api).To(peer).Text(ctx, text)
	if err != nil {
		return SentMessage{}, fmt.Errorf("отправка: %w", err)
	}
	return sentFromUpdates(upd), nil
}

// HistoryByPeer возвращает последние limit сообщений чата по готовому peer
// (старые сверху).
func (s *Session) HistoryByPeer(ctx context.Context, peer tg.InputPeerClass, limit int) ([]HistoryMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []HistoryMessage
	iter := messages.NewQueryBuilder(s.api).GetHistory(peer).BatchSize(limit).Iter()
	for len(out) < limit && iter.Next(ctx) {
		if hm, ok := historyFromElem(iter.Value()); ok {
			out = append(out, hm)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Dialogs возвращает список диалогов (свежие сверху). limit ограничивает число
// строк; onlyUnread оставляет только чаты с непрочитанными сообщениями.
func (s *Session) Dialogs(ctx context.Context, limit int, onlyUnread bool) ([]Dialog, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []Dialog
	iter := query.GetDialogs(s.api).BatchSize(100).Iter()
	scanned := 0
	for len(out) < limit && iter.Next(ctx) {
		// Защита от слишком долгого перебора, когда непрочитанных мало.
		if scanned++; scanned > 1000 {
			break
		}
		d := dialogFromElem(iter.Value())
		if onlyUnread && d.Unread == 0 {
			continue
		}
		out = append(out, d)
	}
	return out, iter.Err()
}

// History возвращает последние limit сообщений чата to (старые сверху).
func (s *Session) History(ctx context.Context, to string, limit int) ([]HistoryMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	target, err := resolveTarget(message.NewSender(s.api), to).AsInputPeer(ctx)
	if err != nil {
		return nil, fmt.Errorf("получатель %q: %w", to, err)
	}
	return s.HistoryByPeer(ctx, target, limit)
}

// SendMessage — одиночная отправка (открывает соединение на операцию).
func (c *Client) SendMessage(ctx context.Context, m Message) (SentMessage, error) {
	var sent SentMessage
	err := c.WithSession(ctx, func(ctx context.Context, s *Session) error {
		var err error
		sent, err = s.Send(ctx, m.To, m.Text)
		return err
	})
	return sent, err
}

// Dialogs — одиночный запрос списка диалогов.
func (c *Client) Dialogs(ctx context.Context, limit int, onlyUnread bool) ([]Dialog, error) {
	var out []Dialog
	err := c.WithSession(ctx, func(ctx context.Context, s *Session) error {
		var err error
		out, err = s.Dialogs(ctx, limit, onlyUnread)
		return err
	})
	return out, err
}

// History — одиночный запрос истории чата.
func (c *Client) History(ctx context.Context, to string, limit int) ([]HistoryMessage, error) {
	var out []HistoryMessage
	err := c.WithSession(ctx, func(ctx context.Context, s *Session) error {
		var err error
		out, err = s.History(ctx, to, limit)
		return err
	})
	return out, err
}

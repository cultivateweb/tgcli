package telegram

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
)

// terminalAuth реализует auth.UserAuthenticator: спрашивает у пользователя
// телефон, код и (при включённой 2FA) пароль через stdin/stdout.
type terminalAuth struct {
	phone  string // если задан в конфиге — не спрашиваем повторно
	reader *bufio.Reader
}

func newTerminalAuth(phone string) auth.UserAuthenticator {
	return &terminalAuth{
		phone:  strings.TrimSpace(phone),
		reader: bufio.NewReader(os.Stdin),
	}
}

func (a *terminalAuth) Phone(_ context.Context) (string, error) {
	if a.phone != "" {
		return a.phone, nil
	}
	return a.prompt("Номер телефона (в формате +71234567890): ")
}

func (a *terminalAuth) Code(_ context.Context, sentCode *tg.AuthSentCode) (string, error) {
	fmt.Println(describeSentCode(sentCode))
	if sentCode != nil {
		if n := codeLength(sentCode.Type); n > 0 {
			fmt.Printf("Это код из %d цифр — вводите только цифры, без букв и дефисов.\n", n)
		}
		if to, ok := sentCode.GetTimeout(); ok && to > 0 {
			fmt.Printf("Код действует ограниченное время: введите его в течение ~%d сек.\n", to)
		}
	}
	return a.prompt("Код из Telegram: ")
}

// codeLength возвращает ожидаемую длину цифрового кода для типов, где она
// известна; 0 — если тип кода её не сообщает.
func codeLength(t tg.AuthSentCodeTypeClass) int {
	switch v := t.(type) {
	case *tg.AuthSentCodeTypeApp:
		return v.Length
	case *tg.AuthSentCodeTypeSMS:
		return v.Length
	case *tg.AuthSentCodeTypeCall:
		return v.Length
	}
	return 0
}

// describeSentCode поясняет, каким каналом Telegram отправил код, чтобы было
// понятно, где его искать (приложение, SMS, звонок и т. п.).
func describeSentCode(sc *tg.AuthSentCode) string {
	if sc == nil {
		return "Telegram отправил код подтверждения."
	}
	switch sc.Type.(type) {
	case *tg.AuthSentCodeTypeApp:
		return "Код отправлен в приложение Telegram — ищите его в чате «Telegram» " +
			"(отправитель 42777) на других ваших устройствах, где вы уже вошли."
	case *tg.AuthSentCodeTypeSMS, *tg.AuthSentCodeTypeSMSWord, *tg.AuthSentCodeTypeSMSPhrase:
		return "Код отправлен по SMS на указанный номер."
	case *tg.AuthSentCodeTypeFragmentSMS:
		return "Код отправлен через Fragment (для анонимных номеров)."
	case *tg.AuthSentCodeTypeFirebaseSMS:
		return "Код отправлен по SMS."
	case *tg.AuthSentCodeTypeCall:
		return "Код продиктуют голосовым звонком."
	case *tg.AuthSentCodeTypeMissedCall:
		return "Код — последние цифры номера, с которого поступит сброшенный звонок."
	case *tg.AuthSentCodeTypeFlashCall:
		return "Код придёт флеш-звонком."
	case *tg.AuthSentCodeTypeEmailCode:
		return "Код отправлен на привязанную электронную почту."
	default:
		return "Telegram отправил код подтверждения."
	}
}

func (a *terminalAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Пароль двухфакторной аутентификации: ")
	// Скрываем ввод пароля, если stdin — терминал.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return a.readLine()
}

// AcceptTermsOfService подтверждает условия сервиса при первичной регистрации.
func (a *terminalAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	fmt.Println(tos.Text)
	return nil
}

// SignUp не поддерживается: tgcli работает с уже существующим аккаунтом.
func (a *terminalAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New(
		"аккаунт не зарегистрирован: зарегистрируйтесь в официальном клиенте Telegram")
}

func (a *terminalAuth) prompt(label string) (string, error) {
	fmt.Print(label)
	return a.readLine()
}

func (a *terminalAuth) readLine() (string, error) {
	line, err := a.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", errors.New("пустой ввод")
	}
	return line, nil
}

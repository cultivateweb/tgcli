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

func (a *terminalAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	return a.prompt("Код из Telegram: ")
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

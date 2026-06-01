package telegram

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/tg"
	"golang.org/x/term"
)

// prompter спрашивает у пользователя телефон, код и (при 2FA) пароль через
// stdin/stdout. Используется ручным flow входа (см. signIn в client.go).
type prompter struct {
	phone  string // если задан в конфиге/окружении — не спрашиваем
	reader *bufio.Reader
}

func newPrompter(phone string) *prompter {
	return &prompter{
		phone:  strings.TrimSpace(phone),
		reader: bufio.NewReader(os.Stdin),
	}
}

// Phone возвращает номер из конфигурации или спрашивает его у пользователя.
func (p *prompter) Phone() (string, error) {
	if p.phone != "" {
		return p.phone, nil
	}
	return p.readNonEmpty("Номер телефона (в формате +71234567890): ")
}

// Password читает 2FA-пароль, скрывая ввод, если stdin — терминал.
func (p *prompter) Password() (string, error) {
	fmt.Print("Пароль двухфакторной аутентификации: ")
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return p.line()
}

// Code печатает, каким каналом отправлен код, и читает ответ. Пустой ввод
// или «r»/«resend» означает запрос повторной отправки (resend=true).
func (p *prompter) Code(sc *tg.AuthSentCode) (code string, resend bool, err error) {
	fmt.Println(describeSentCode(sc))
	if sc != nil {
		if n := codeLength(sc.Type); n > 0 {
			fmt.Printf("Это код из %d цифр — вводите только цифры, без букв и дефисов.\n", n)
		}
		if to, ok := sc.GetTimeout(); ok && to > 0 {
			fmt.Printf("Код действует ограниченное время: введите его в течение ~%d сек.\n", to)
		}
	}
	fmt.Print("Код из Telegram (Enter или «r» — прислать заново): ")
	line, err := p.line()
	if err != nil {
		return "", false, err
	}
	switch strings.ToLower(line) {
	case "", "r", "resend", "повтор":
		return "", true, nil
	default:
		return line, false, nil
	}
}

// readNonEmpty печатает приглашение и читает непустую строку.
func (p *prompter) readNonEmpty(label string) (string, error) {
	fmt.Print(label)
	line, err := p.line()
	if err != nil {
		return "", err
	}
	if line == "" {
		return "", errors.New("пустой ввод")
	}
	return line, nil
}

// line читает строку из stdin и обрезает пробелы/перевод строки.
func (p *prompter) line() (string, error) {
	line, err := p.reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
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

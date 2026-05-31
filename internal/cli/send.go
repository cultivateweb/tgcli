package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func sendCmd() *Command {
	var to string
	return &Command{
		Name:    "send",
		Summary: "отправить текстовое сообщение",
		Usage:   appName + ` send --to <@username|me> <текст>   (или текст из stdin)`,
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&to, "to", "", "получатель: @username, me (Избранное) или номер телефона")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if to == "" {
				return fmt.Errorf("укажите получателя через --to")
			}

			text, err := messageText(args)
			if err != nil {
				return err
			}

			client := telegram.New(env.Config)
			msg, err := client.SendMessage(ctx, telegram.Message{To: to, Text: text})
			if err != nil {
				return err
			}
			if env.Verbose && msg.ID != 0 {
				fmt.Printf("Отправлено сообщение #%d → %s\n", msg.ID, to)
			} else {
				fmt.Println("Отправлено.")
			}
			return nil
		},
	}
}

// messageText берёт текст из позиционных аргументов, а если их нет и stdin
// не терминал — читает тело из stdin (для пайпов: echo ... | tgcli send).
func messageText(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	if hasStdinData() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		text := strings.TrimRight(string(data), "\n")
		if text == "" {
			return "", fmt.Errorf("пустой ввод из stdin")
		}
		return text, nil
	}
	return "", fmt.Errorf("укажите текст сообщения или передайте его через stdin")
}

// hasStdinData сообщает, перенаправлен ли stdin (пайп/файл), а не терминал.
func hasStdinData() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) == 0
}

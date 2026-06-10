package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func readCmd() *Command {
	var (
		chat   string
		limit  int
		asJSON bool
	)
	return &Command{
		Name:    "read",
		Summary: "показать последние сообщения чата",
		Usage:   appName + " read --chat <@username|me> [--limit N] [--json]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&chat, "chat", "", "чат: @username, me (Избранное) или телефон")
			fs.IntVar(&limit, "limit", 20, "сколько сообщений показать")
			fs.BoolVar(&asJSON, "json", false, "вывести в формате JSON")
		},
		Run: func(ctx context.Context, env *Env, _ []string) error {
			if chat == "" {
				return fmt.Errorf("укажите чат через --chat")
			}
			client := telegram.New(env.Config)
			msgs, err := client.History(ctx, chat, limit)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(msgs)
			}
			if len(msgs) == 0 {
				fmt.Println("Сообщений нет.")
				return nil
			}
			for _, m := range msgs {
				text := strings.ReplaceAll(m.Text, "\n", " ")
				if text == "" {
					text = "[вложение]"
				}
				if m.Fwd != nil { // пересланное — помечаем источник
					text = "↪ из " + m.Fwd.Origin + ": " + text
				}
				fmt.Printf("%s  %-20s  %s\n", formatTime(m.Date), m.Author, text)
			}
			return nil
		},
	}
}

package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func authCmd() *Command {
	var logout bool
	return &Command{
		Name:    "auth",
		Summary: "войти в аккаунт Telegram или выйти из него",
		Usage:   appName + " auth [--logout]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&logout, "logout", false, "завершить сессию и удалить сохранённые данные")
		},
		Run: func(ctx context.Context, env *Env, _ []string) error {
			client := telegram.New(env.Config)

			if logout {
				if err := client.Logout(ctx); err != nil {
					return fmt.Errorf("выход: %w", err)
				}
				fmt.Println("Сессия завершена.")
				return nil
			}

			self, err := client.Login(ctx)
			if err != nil {
				return fmt.Errorf("вход: %w", err)
			}
			fmt.Printf("Авторизация выполнена: %s.\n", telegram.DisplayName(self))
			return nil
		},
	}
}

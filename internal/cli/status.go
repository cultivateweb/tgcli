package cli

import (
	"context"
	"fmt"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func statusCmd() *Command {
	return &Command{
		Name:    "status",
		Summary: "показать состояние авторизации",
		Usage:   appName + " status",
		Run: func(ctx context.Context, env *Env, _ []string) error {
			client := telegram.New(env.Config)
			authorized, self, err := client.Status(ctx)
			if err != nil {
				return err
			}
			if !authorized {
				fmt.Println("Не авторизован. Выполните «tgcli auth».")
				return nil
			}
			fmt.Printf("Авторизован: %s\n", telegram.DisplayName(self))
			return nil
		},
	}
}

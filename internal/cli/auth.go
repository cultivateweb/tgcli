package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func authCmd() *Command {
	var (
		logout bool
		qr     bool
	)
	return &Command{
		Name:    "auth",
		Summary: "войти в аккаунт Telegram или выйти из него",
		Usage:   appName + " auth [--qr] [--logout]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&logout, "logout", false, "завершить сессию и удалить сохранённые данные")
			fs.BoolVar(&qr, "qr", false, "войти по QR-коду (сканировать в приложении, без SMS/кода)")
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

			// Вход интерактивный: нужен терминал для QR/кода и 2FA-пароля.
			// Без терминала чтение stdin сразу даёт EOF — даём понятную ошибку.
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("для входа нужен интерактивный терминал.\n" +
					"Откройте обычное окно терминала и выполните «tgcli auth».\n" +
					"Если вы в Claude Code — не запускайте команду через префикс «!»: там нет ввода с клавиатуры")
			}

			login := client.Login
			if qr {
				login = client.LoginQR
			}
			self, err := login(ctx)
			if err != nil {
				return fmt.Errorf("вход: %w", err)
			}
			fmt.Printf("Авторизация выполнена: %s.\n", telegram.DisplayName(self))
			return nil
		},
	}
}

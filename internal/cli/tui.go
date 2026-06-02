package cli

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/cultivateweb/tgcli/internal/cache"
	"github.com/cultivateweb/tgcli/internal/telegram"
	"github.com/cultivateweb/tgcli/internal/tui"
)

func tuiCmd() *Command {
	return &Command{
		Name:    "tui",
		Summary: "интерактивный интерфейс (список чатов, переписка, ввод)",
		Usage:   appName + " tui",
		Run: func(ctx context.Context, env *Env, _ []string) error {
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("tui требует интерактивный терминал")
			}
			// Локальный кеш диалогов/истории — TUI открывается мгновенно.
			// Если кеш не открылся, работаем без него (не критично).
			var c *cache.Cache
			if opened, err := cache.Open(env.Config.CachePath()); err == nil {
				c = opened
				defer c.Close()
			}

			client := telegram.New(env.Config)
			// Держим одно соединение всё время работы интерфейса.
			return client.WithSession(ctx, func(ctx context.Context, s *telegram.Session) error {
				return tui.Run(ctx, s, c)
			})
		},
	}
}

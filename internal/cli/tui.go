package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/cultivateweb/tgcli/internal/cache"
	"github.com/cultivateweb/tgcli/internal/telegram"
	"github.com/cultivateweb/tgcli/internal/tui"
)

func tuiCmd() *Command {
	var themeName string
	return &Command{
		Name:    "tui",
		Summary: "интерактивный интерфейс (список чатов, переписка, ввод)",
		Usage:   appName + " tui [--theme tokyo|mocha|gruvbox|nord]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&themeName, "theme", "", "цветовая тема: tokyo, mocha, gruvbox, nord")
		},
		Run: func(ctx context.Context, env *Env, _ []string) error {
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("tui требует интерактивный терминал")
			}
			// Флаг переопределяет сохранённую тему на этот запуск; F8/меню
			// поверх него по-прежнему сохраняют выбор в конфиг.
			if themeName != "" {
				canonical, ok := tui.ResolveThemeName(themeName)
				if !ok {
					return fmt.Errorf("неизвестная тема %q (доступны: tokyo, mocha, gruvbox, nord)", themeName)
				}
				env.Config.Theme = canonical
			}
			// Локальный кеш диалогов/истории — TUI открывается мгновенно.
			// Если кеш не открылся, работаем без него (не критично).
			var c *cache.Cache
			if opened, err := cache.Open(env.Config.CachePath()); err == nil {
				c = opened
				defer c.Close()
			}

			client := telegram.New(env.Config)
			// Держим одно соединение всё время работы интерфейса; с потоком
			// обновлений входящие сообщения прилетают в TUI сами.
			return client.WithLiveSession(ctx, func(ctx context.Context, s *telegram.Session, updates <-chan telegram.NewMessage) error {
				return tui.Run(ctx, s, c, env.Config, updates, env.Version)
			})
		},
	}
}

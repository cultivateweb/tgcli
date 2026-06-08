package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/cultivateweb/tgcli/internal/daemon"
	"github.com/cultivateweb/tgcli/internal/ipc"
)

func daemonCmd() *Command {
	var stop bool
	return &Command{
		Name:    "daemon",
		Summary: "фоновый резидент: держит онлайн и раздаёт события",
		Usage:   appName + " daemon [--stop]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&stop, "stop", false, "остановить запущенный демон")
		},
		Run: func(ctx context.Context, env *Env, _ []string) error {
			socket := env.Config.SocketPath()
			if stop {
				if err := ipc.Stop(socket); err != nil {
					return err
				}
				fmt.Println("Демон остановлен.")
				return nil
			}
			// Резидент работает на переднем плане и блокируется до Ctrl+C /
			// SIGTERM или «tgcli daemon --stop». Фоновость — за окружением.
			return daemon.New(env.Config).Run(ctx)
		},
	}
}

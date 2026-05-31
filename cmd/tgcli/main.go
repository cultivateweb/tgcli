// Command tgcli — точка входа CLI-клиента Telegram.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cultivateweb/tgcli/internal/cli"
)

// version подставляется при сборке через -ldflags (см. Makefile).
var version = "dev"

func main() {
	// Контекст, который отменяется по Ctrl+C / SIGTERM — чтобы
	// длительные операции (long-polling, загрузки) можно было прервать.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cli.Run(ctx, version, os.Args[1:]))
}

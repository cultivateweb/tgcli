package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cultivateweb/tgcli/internal/ipc"
)

func eventsCmd() *Command {
	var asJSON bool
	return &Command{
		Name:    "events",
		Summary: "стрим новых сообщений от демона",
		Usage:   appName + " events [--json]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&asJSON, "json", false, "по строке JSON на событие (для скриптов)")
		},
		Run: func(ctx context.Context, env *Env, _ []string) error {
			enc := json.NewEncoder(os.Stdout)
			err := ipc.Subscribe(ctx, env.Config.SocketPath(), func(ev ipc.MessageEvent) {
				if asJSON {
					_ = enc.Encode(ev)
					return
				}
				fmt.Println(formatEvent(ev))
			})
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
}

// formatEvent — человекочитаемая строка события для stdout.
func formatEvent(ev ipc.MessageEvent) string {
	text := ev.Text
	if ev.Media != "" {
		if text != "" {
			text += " "
		}
		text += "📎 " + ev.Media
	}
	arrow := "←"
	if ev.Out {
		arrow = "→"
	}
	return fmt.Sprintf("%s %s %s: %s", ev.Time.Local().Format("15:04:05"), arrow, ev.From, text)
}

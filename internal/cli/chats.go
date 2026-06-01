package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func chatsCmd() *Command {
	var (
		limit  int
		unread bool
		asJSON bool
	)
	return &Command{
		Name:    "chats",
		Summary: "показать список диалогов",
		Usage:   appName + " chats [--limit N] [--unread] [--json]",
		Flags: func(fs *flag.FlagSet) {
			fs.IntVar(&limit, "limit", 20, "сколько диалогов показать")
			fs.BoolVar(&unread, "unread", false, "только чаты с непрочитанными")
			fs.BoolVar(&asJSON, "json", false, "вывести в формате JSON")
		},
		Run: func(ctx context.Context, env *Env, _ []string) error {
			client := telegram.New(env.Config)
			dialogs, err := client.Dialogs(ctx, limit, unread)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(dialogs)
			}
			if len(dialogs) == 0 {
				fmt.Println("Диалогов нет.")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, d := range dialogs {
				mark := ""
				if d.Unread > 0 {
					mark = fmt.Sprintf("●%d", d.Unread)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					mark, formatTime(d.Date), d.Kind, d.Title, d.Preview)
			}
			return tw.Flush()
		},
	}
}

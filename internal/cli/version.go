package cli

import (
	"context"
	"fmt"
	"runtime"
)

func versionCmd() *Command {
	return &Command{
		Name:    "version",
		Summary: "показать версию tgcli",
		Usage:   appName + " version",
		Run: func(_ context.Context, env *Env, _ []string) error {
			fmt.Printf("%s %s (%s/%s, %s)\n",
				appName, env.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
			return nil
		},
	}
}

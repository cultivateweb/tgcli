// Package cli разбирает аргументы командной строки и направляет вызов
// в нужную подкоманду. Это лёгкий самописный диспетчер на стандартной
// библиотеке — без внешних зависимостей.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/cultivateweb/tgcli/internal/config"
)

const appName = "tgcli"

// commands — реестр всех подкоманд. Новую команду достаточно добавить сюда.
func commands() []*Command {
	return []*Command{
		authCmd(),
		chatsCmd(),
		readCmd(),
		sendCmd(),
		statusCmd(),
		tuiCmd(),
		versionCmd(),
	}
}

// Run — основная точка входа CLI. Возвращает код выхода процесса.
func Run(ctx context.Context, version string, args []string) int {
	var (
		verbose    bool
		configPath string
	)

	root := flag.NewFlagSet(appName, flag.ContinueOnError)
	root.BoolVar(&verbose, "v", false, "подробный вывод (verbose)")
	root.StringVar(&configPath, "config", "", "путь к файлу конфигурации (по умолчанию: $HOME/.config/tgcli/config.json)")
	root.Usage = printRootUsage

	if err := root.Parse(args); err != nil {
		// flag сам печатает ошибку и обрабатывает -h.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	rest := root.Args()
	if len(rest) == 0 {
		printRootUsage()
		return 2
	}

	name, cmdArgs := rest[0], rest[1:]

	if name == "help" {
		return runHelp(cmdArgs)
	}

	cmd := lookup(name)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "%s: неизвестная команда %q\n", appName, name)
		fmt.Fprintf(os.Stderr, "Запустите «%s help» для списка команд.\n", appName)
		return 2
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: не удалось загрузить конфигурацию: %v\n", appName, err)
		return 1
	}

	env := &Env{Config: cfg, Version: version, Verbose: verbose}

	fs := flag.NewFlagSet(appName+" "+cmd.Name, flag.ContinueOnError)
	fs.Usage = func() { printCommandUsage(cmd, fs) }
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	if err := fs.Parse(cmdArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if err := cmd.Run(ctx, env, fs.Args()); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "прервано")
			return 130 // 128 + SIGINT
		}
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", appName, cmd.Name, err)
		return 1
	}
	return 0
}

func lookup(name string) *Command {
	for _, c := range commands() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func runHelp(args []string) int {
	if len(args) == 0 {
		printRootUsage()
		return 0
	}
	cmd := lookup(args[0])
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "%s: неизвестная команда %q\n", appName, args[0])
		return 2
	}
	fs := flag.NewFlagSet(appName+" "+cmd.Name, flag.ContinueOnError)
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	printCommandUsage(cmd, fs)
	return 0
}

func printRootUsage() {
	w := os.Stderr
	fmt.Fprintf(w, "%s — CLI-клиент Telegram\n\n", appName)
	fmt.Fprintf(w, "Использование:\n  %s [глобальные флаги] <команда> [аргументы]\n\n", appName)

	fmt.Fprintln(w, "Команды:")
	cmds := commands()
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range cmds {
		fmt.Fprintf(tw, "  %s\t%s\n", c.Name, c.Summary)
	}
	tw.Flush()

	fmt.Fprintln(w, "\nГлобальные флаги:")
	gw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(gw, "  -v\tподробный вывод (verbose)")
	fmt.Fprintln(gw, "  -config <путь>\tфайл конфигурации (по умолчанию $HOME/.config/tgcli/config.json)")
	gw.Flush()

	fmt.Fprintf(w, "\nПодробнее о команде: %s help <команда>\n", appName)
}

func printCommandUsage(cmd *Command, fs *flag.FlagSet) {
	w := os.Stderr
	fmt.Fprintf(w, "%s\n\n", cmd.Summary)
	usage := cmd.Usage
	if usage == "" {
		usage = appName + " " + cmd.Name
	}
	fmt.Fprintf(w, "Использование:\n  %s\n", usage)

	// Подсчитаем, есть ли у команды флаги, чтобы не печатать пустой заголовок.
	hasFlags := false
	fs.VisitAll(func(*flag.Flag) { hasFlags = true })
	if hasFlags {
		fmt.Fprintln(w, "\nФлаги:")
		fs.PrintDefaults()
	}
}

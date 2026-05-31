package cli

import (
	"context"
	"flag"

	"github.com/cultivateweb/tgcli/internal/config"
)

// Command — одна подкоманда CLI (например, `tgcli send`).
//
// Каждая команда сама описывает свои флаги в Flags и выполняет работу в Run.
// Такой минимальный интерфейс легко заменить на cobra/urfave, если проект
// дорастёт до сложного дерева команд.
type Command struct {
	Name    string // имя для вызова: tgcli <Name>
	Summary string // короткое описание для общего списка справки
	Usage   string // строка использования, показывается в `tgcli help <Name>`

	// Flags регистрирует флаги команды в переданном FlagSet.
	// Может быть nil, если у команды нет собственных флагов.
	Flags func(fs *flag.FlagSet)

	// Run выполняет команду. args — позиционные аргументы после разбора флагов.
	Run func(ctx context.Context, env *Env, args []string) error
}

// Env — общее окружение, доступное каждой команде:
// загруженная конфигурация и глобальные параметры.
type Env struct {
	Config  *config.Config
	Version string
	Verbose bool
}

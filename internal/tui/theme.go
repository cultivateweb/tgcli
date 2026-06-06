package tui

// Цветовые темы интерфейса. Все цвета приложения собраны здесь в одну структуру
// Theme с семантическими ролями (фон, текст, ошибка, …), а не «сырыми» оттенками.
// Это позволяет переключать палитру на лету (F8 или меню «Вид») и хранить выбор
// в конфиге. Виджеты tview захватывают tview.Styles в момент создания, поэтому
// при смене темы их перекрашивает ui.restyle (см. tui.go); примитивы, рисующие
// себя сами (msgList, dialog, меню), читают theme.* прямо в Draw и подхватывают
// новую палитру при ближайшей перерисовке.

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Theme — палитра приложения. Цвета хранятся как hex-строки с «#»: они нужны и
// для tcell.GetColor, и для интерполяции в color-теги tview («[#rrggbb]…[-]»).
type Theme struct {
	Name string // отображается в меню «Вид»

	// Каркас.
	BgWindow   string // фон корневого контейнера
	BgPanel    string // фон панелей и диалогов (tview.Styles.PrimitiveBackgroundColor)
	Text       string // основной текст, тело сообщений (PrimaryTextColor)
	TextBright string // яркий текст/заголовки (SecondaryTextColor)
	TextDim    string // приглушённый: время, подсказки (TertiaryTextColor)
	Inverse    string // инверсный текст для focusTitle (InverseTextColor)

	// Рамки и фокус.
	BorderActive string // рамка активной панели
	TitleActive  string // заголовок активной панели
	Inactive     string // рамка и заголовок неактивных панелей

	// Бары, меню, окна (стиль Turbo Vision).
	BarBg     string // фон меню-бара и статус-бара
	BarFg     string // текст пунктов бара
	BarAccel  string // горячая буква в баре/статусе
	MenuText  string // текст пункта выпадающего меню
	MenuSelBg string // выделенный пункт меню — фон
	MenuSelFg string // выделенный пункт меню — текст
	MenuAccel string // акселератор в пункте меню
	Shadow    string // фон тени окна
	ShadowFg  string // символы тени
	Scroll    string // скроллбар

	Contrast     string // tview.Styles.ContrastBackgroundColor
	MoreContrast string // tview.Styles.MoreContrastBackgroundColor

	// Лента сообщений.
	MsgBg     string // фон чётных строк (зебра)
	MsgBgAlt  string // фон нечётных строк
	MsgSel    string // выбранное сообщение — полоса-курсор
	MsgOut    string // знак исходящего «→»
	MsgAuthor string // имя автора
	MsgCode   string // код в сообщении / метка вложения
	MsgLink   string // ссылка

	// Статус-строка.
	Success string // зелёный («Скопировано», «Открыто»)
	ErrorC  string // красный («Ошибка…»)
	Warn    string // оранжевый/жёлтый (предупреждения, пауза)
	Info    string // голубой («⬇ Загрузка…»)
	Accent  string // акцент «● НОВОЕ»

	// Чаты (по типам).
	Self, User, Bot, Group, Supergroup, Channel, Unread string
}

// theme — текущая активная палитра пакета. Меняется applyTheme.
var theme = tokyoNight

// themes — список доступных тем в порядке перебора по F8 и в меню «Вид».
var themes = []Theme{tokyoNight, mocha, gruvbox, nord}

// tokyoNight — тема по умолчанию: приглушённый сине-фиолетовый фон, мягкий текст.
var tokyoNight = Theme{
	Name:         "Tokyo Night",
	BgWindow:     "#1a1b26",
	BgPanel:      "#24283b",
	Text:         "#c0caf5",
	TextBright:   "#ffffff",
	TextDim:      "#565f89",
	Inverse:      "#1a1b26",
	BorderActive: "#7aa2f7",
	TitleActive:  "#c0caf5",
	Inactive:     "#414868",
	BarBg:        "#292e42",
	BarFg:        "#c0caf5",
	BarAccel:     "#ff9e64",
	MenuText:     "#c0caf5",
	MenuSelBg:    "#7aa2f7",
	MenuSelFg:    "#1a1b26",
	MenuAccel:    "#e0af68",
	Shadow:       "#16161e",
	ShadowFg:     "#414868",
	Scroll:       "#3d59a1",
	Contrast:     "#3d59a1",
	MoreContrast: "#7aa2f7",
	MsgBg:        "#24283b",
	MsgBgAlt:     "#1f2335",
	MsgSel:       "#2f3549",
	MsgOut:       "#9ece6a",
	MsgAuthor:    "#7dcfff",
	MsgCode:      "#e0af68",
	MsgLink:      "#7aa2f7",
	Success:      "#9ece6a",
	ErrorC:       "#f7768e",
	Warn:         "#e0af68",
	Info:         "#7dcfff",
	Accent:       "#bb9af7",
	Self:         "#7dcfff",
	User:         "#9ece6a",
	Bot:          "#e0af68",
	Group:        "#7aa2f7",
	Supergroup:   "#2ac3de",
	Channel:      "#bb9af7",
	Unread:       "#ff9e64",
}

// mocha — Catppuccin Mocha: тёплый тёмный фон, нежные пастельные акценты.
var mocha = Theme{
	Name:         "Catppuccin Mocha",
	BgWindow:     "#1e1e2e",
	BgPanel:      "#313244",
	Text:         "#cdd6f4",
	TextBright:   "#ffffff",
	TextDim:      "#a6adc8",
	Inverse:      "#1e1e2e",
	BorderActive: "#89b4fa",
	TitleActive:  "#cdd6f4",
	Inactive:     "#45475a",
	BarBg:        "#181825",
	BarFg:        "#cdd6f4",
	BarAccel:     "#fab387",
	MenuText:     "#cdd6f4",
	MenuSelBg:    "#89b4fa",
	MenuSelFg:    "#1e1e2e",
	MenuAccel:    "#f9e2af",
	Shadow:       "#11111b",
	ShadowFg:     "#45475a",
	Scroll:       "#585b70",
	Contrast:     "#585b70",
	MoreContrast: "#89b4fa",
	MsgBg:        "#313244",
	MsgBgAlt:     "#292c3c",
	MsgSel:       "#45475a",
	MsgOut:       "#a6e3a1",
	MsgAuthor:    "#89dceb",
	MsgCode:      "#fab387",
	MsgLink:      "#89b4fa",
	Success:      "#a6e3a1",
	ErrorC:       "#f38ba8",
	Warn:         "#f9e2af",
	Info:         "#89dceb",
	Accent:       "#cba6f7",
	Self:         "#89dceb",
	User:         "#a6e3a1",
	Bot:          "#fab387",
	Group:        "#89b4fa",
	Supergroup:   "#74c7ec",
	Channel:      "#cba6f7",
	Unread:       "#fab387",
}

// gruvbox — Gruvbox Dark: тёплая землисто-ретро гамма, мягкий «винтаж».
var gruvbox = Theme{
	Name:         "Gruvbox Dark",
	BgWindow:     "#282828",
	BgPanel:      "#3c3836",
	Text:         "#ebdbb2",
	TextBright:   "#fbf1c7",
	TextDim:      "#928374",
	Inverse:      "#282828",
	BorderActive: "#83a598",
	TitleActive:  "#ebdbb2",
	Inactive:     "#504945",
	BarBg:        "#1d2021",
	BarFg:        "#ebdbb2",
	BarAccel:     "#fe8019",
	MenuText:     "#ebdbb2",
	MenuSelBg:    "#83a598",
	MenuSelFg:    "#1d2021",
	MenuAccel:    "#fabd2f",
	Shadow:       "#1d2021",
	ShadowFg:     "#504945",
	Scroll:       "#665c54",
	Contrast:     "#504945",
	MoreContrast: "#83a598",
	MsgBg:        "#3c3836",
	MsgBgAlt:     "#32302f",
	MsgSel:       "#504945",
	MsgOut:       "#b8bb26",
	MsgAuthor:    "#8ec07c",
	MsgCode:      "#fabd2f",
	MsgLink:      "#83a598",
	Success:      "#b8bb26",
	ErrorC:       "#fb4934",
	Warn:         "#fabd2f",
	Info:         "#8ec07c",
	Accent:       "#d3869b",
	Self:         "#8ec07c",
	User:         "#b8bb26",
	Bot:          "#fabd2f",
	Group:        "#83a598",
	Supergroup:   "#83a598",
	Channel:      "#d3869b",
	Unread:       "#fe8019",
}

// nord — Nord: холодная спокойная сине-серая гамма, минимум ярких пятен.
var nord = Theme{
	Name:         "Nord",
	BgWindow:     "#2e3440",
	BgPanel:      "#3b4252",
	Text:         "#d8dee9",
	TextBright:   "#eceff4",
	TextDim:      "#616e88",
	Inverse:      "#2e3440",
	BorderActive: "#88c0d0",
	TitleActive:  "#eceff4",
	Inactive:     "#434c5e",
	BarBg:        "#272c36",
	BarFg:        "#d8dee9",
	BarAccel:     "#d08770",
	MenuText:     "#d8dee9",
	MenuSelBg:    "#88c0d0",
	MenuSelFg:    "#2e3440",
	MenuAccel:    "#ebcb8b",
	Shadow:       "#21252e",
	ShadowFg:     "#434c5e",
	Scroll:       "#4c566a",
	Contrast:     "#434c5e",
	MoreContrast: "#88c0d0",
	MsgBg:        "#3b4252",
	MsgBgAlt:     "#343b48",
	MsgSel:       "#434c5e",
	MsgOut:       "#a3be8c",
	MsgAuthor:    "#8fbcbb",
	MsgCode:      "#ebcb8b",
	MsgLink:      "#81a1c1",
	Success:      "#a3be8c",
	ErrorC:       "#bf616a",
	Warn:         "#ebcb8b",
	Info:         "#88c0d0",
	Accent:       "#b48ead",
	Self:         "#8fbcbb",
	User:         "#a3be8c",
	Bot:          "#ebcb8b",
	Group:        "#81a1c1",
	Supergroup:   "#88c0d0",
	Channel:      "#b48ead",
	Unread:       "#d08770",
}

// themeByName возвращает тему по имени (Theme.Name). Для пустого/неизвестного
// имени — тема по умолчанию tokyoNight.
func themeByName(name string) Theme {
	for _, t := range themes {
		if t.Name == name {
			return t
		}
	}
	return tokyoNight
}

// kindColor возвращает цвет чата по его типу (категории аккордеона). Неизвестный
// тип получает яркий текст — на случай новых видов чатов.
func kindColor(key string) string {
	switch key {
	case "unread":
		return theme.Unread
	case "self":
		return theme.Self
	case "user":
		return theme.User
	case "bot":
		return theme.Bot
	case "group", "mygroup":
		return theme.Group
	case "supergroup":
		return theme.Supergroup
	case "channel", "mychannel":
		return theme.Channel
	default:
		return theme.TextBright
	}
}

// applyTheme делает t активной темой и переносит её в глобальную палитру tview.
// Должна вызываться до создания виджетов (на старте) либо в паре с ui.restyle
// (при переключении на лету), т.к. виджеты копируют tview.Styles при создании.
func applyTheme(t Theme) {
	theme = t
	hex := tcell.GetColor
	tview.Styles.PrimitiveBackgroundColor = hex(t.BgPanel)
	tview.Styles.ContrastBackgroundColor = hex(t.Contrast)
	tview.Styles.MoreContrastBackgroundColor = hex(t.MoreContrast)
	tview.Styles.BorderColor = hex(t.Inactive)
	tview.Styles.TitleColor = hex(t.Inactive)
	tview.Styles.PrimaryTextColor = hex(t.Text)
	tview.Styles.SecondaryTextColor = hex(t.TextBright)
	tview.Styles.TertiaryTextColor = hex(t.TextDim)
	tview.Styles.InverseTextColor = hex(t.Inverse)
}

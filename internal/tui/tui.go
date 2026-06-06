// Package tui реализует интерактивный интерфейс tgcli на tview.
//
// Раскладка: сверху строка с именем/версией, по центру — аккордеон чатов слева
// (сгруппированы по типам), переписка и поле ввода в середине, панель деталей
// справа (прячется), статус снизу. Навигация, прокрутка и фокус — средствами
// tview; ширину символов считает tcell, что избавляет от перекоса рамок.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"

	"github.com/cultivateweb/tgcli/internal/cache"
	"github.com/cultivateweb/tgcli/internal/config"
	"github.com/cultivateweb/tgcli/internal/telegram"
)

// Категории аккордеона в порядке отображения. Saved Messages выводится
// отдельным узлом (см. buildTree), поэтому "self" здесь нет.
var kindOrder = []struct{ key, label string }{
	{"user", "👤 Люди"},
	{"bot", "🤖 Боты"},
	{"group", "👥 Группы"},
	{"mygroup", "👥 Мои группы"},
	{"channel", "📣 Каналы"},
	{"mychannel", "📣 Мои каналы"},
}

func groupKey(d telegram.Dialog) string {
	if d.Ref.Type == "self" || d.Kind == "self" {
		return "self"
	}
	switch d.Kind {
	case "bot":
		return "bot"
	case "group", "supergroup":
		if d.Mine {
			return "mygroup"
		}
		return "group"
	case "channel":
		if d.Mine {
			return "mychannel"
		}
		return "channel"
	default:
		return "user"
	}
}

type ui struct {
	ctx     context.Context
	sess    *telegram.Session
	cache   *cache.Cache
	cfg     *config.Config
	version string

	app      *tview.Application
	pages    *tview.Pages
	tree     *tview.TreeView
	messages *msgList
	input    *tview.TextArea
	details  *tview.List
	status   *tview.TextView
	header   *tview.TextView // верхний меню-бар (CUA)
	mid      *tview.Flex
	center   *tview.Flex

	lastPanel  tview.Primitive // последняя панель в фокусе — куда вернуться после меню
	menuX      []int           // X-координаты пунктов меню-бара (для выпадения)
	menuActive int             // индекс открытого меню или -1
	menuGuard  bool            // подавляет реакцию на программную смену подсветки бара

	detailValues []string // значения элементов панели «Детали» (для копирования)
	msgTitle     string   // базовый заголовок панели сообщений (без стиля фокуса)
	detailsTitle string   // базовый заголовок панели деталей (без стиля фокуса)

	dialogs     []telegram.Dialog
	open        *telegram.Dialog
	history     []telegram.HistoryMessage
	showTree    bool
	showDetails bool

	msgSel   int          // индекс выбранного сообщения
	msgElem  int          // индекс элемента внутри сообщения (Tab: текст/ссылка/вложение)
	selected map[int]bool // мультивыбор сообщений

	forumLoaded map[string]bool     // ключи супергрупп-форумов, чьи темы загружены
	downloads   map[int64]*download // активные загрузки вложений (по ID сообщения)
	treeWidth   int                 // текущая внутренняя ширина панели «Чаты» (для выравнивания)
	favNode     *tview.TreeNode     // узел категории «★ Избранное» (для удаления закладок по d)
}

// Run строит и запускает интерфейс. c, cfg и updates могут быть nil.
func Run(ctx context.Context, sess *telegram.Session, c *cache.Cache, cfg *config.Config, updates <-chan telegram.NewMessage, version string) error {
	name := ""
	if cfg != nil {
		name = cfg.Theme
	}
	applyTheme(themeByName(name)) // сохранённая тема или tokyoNight по умолчанию
	u := &ui{ctx: ctx, sess: sess, cache: c, cfg: cfg, version: version,
		app: tview.NewApplication(), selected: map[int]bool{}, treeWidth: 48,
		forumLoaded: map[string]bool{}, downloads: map[int64]*download{}, menuActive: -1}
	u.build()
	u.pages = tview.NewPages().AddPage("main", u.root(), true, true)

	// Стартуем на полноэкранной панели «Чаты»; переписка открывается по Enter.
	go u.loadDialogs()
	if updates != nil {
		go u.listenUpdates(updates)
	}

	// Мышь намеренно отключена — управление только с клавиатуры.
	u.app.SetRoot(u.pages, true)
	u.app.SetFocus(u.tree)
	u.startDiagnostics() // ловит зависания и пишет дамп горутин в /tmp
	return u.app.Run()
}

// focusBox — общий интерфейс панелей tview (все встраивают *tview.Box).
type focusBox interface {
	SetBorderColor(tcell.Color) *tview.Box
	SetTitleColor(tcell.Color) *tview.Box
	SetBackgroundColor(tcell.Color) *tview.Box
	SetFocusFunc(func()) *tview.Box
	SetBlurFunc(func()) *tview.Box
	HasFocus() bool
}

// Базовые (статические) заголовки панелей.
const (
	titleTree  = " Чаты "
	titleInput = " Ввод "
)

// focusTitle оборачивает заголовок активной панели в инверсный стиль окна
// Turbo Vision: тёмный текст на голубом фоне. Это однозначный признак фокуса,
// не зависящий от того, отличает ли терминал двойную рамку (═) от одинарной (─).
func focusTitle(base string) string {
	return fmt.Sprintf("[%s:%s:b]%s[-:-:-]", theme.Inverse, theme.BorderActive, base)
}

// titledBox — панель, которой можно сменить заголовок и спросить про фокус.
type titledBox interface {
	SetTitle(string) *tview.Box
	HasFocus() bool
}

// setTitle ставит заголовок панели с учётом текущего фокуса (для динамических
// заголовков, которые меняются вне focus/blur-колбэков).
func (u *ui) setTitle(b titledBox, base string) {
	if b.HasFocus() {
		b.SetTitle(focusTitle(base))
	} else {
		b.SetTitle(base)
	}
}

// markFocus подсвечивает панель при фокусе (яркая рамка, белый заголовок) и
// гасит её при потере фокуса (серая рамка и заголовок). onFocus/onBlur —
// необязательные дополнительные действия (например, смена строки состояния).
func markFocus(b focusBox, onFocus, onBlur func()) {
	b.SetFocusFunc(func() {
		b.SetBorderColor(tcell.GetColor(theme.BorderActive))
		b.SetTitleColor(tcell.GetColor(theme.TitleActive))
		if onFocus != nil {
			onFocus()
		}
	})
	b.SetBlurFunc(func() {
		b.SetBorderColor(tcell.GetColor(theme.Inactive))
		b.SetTitleColor(tcell.GetColor(theme.Inactive))
		if onBlur != nil {
			onBlur()
		}
	})
}

func (u *ui) build() {
	hex := func(s string) tcell.Color { return tcell.GetColor(s) }

	u.tree = tview.NewTreeView()
	u.tree.SetBorder(true).SetTitle(titleTree)
	u.tree.SetGraphics(true)
	u.tree.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// Ширина панели динамическая; при её изменении (старт/ресайз) пересобираем
		// дерево, чтобы счётчики снова встали ровно по правому краю.
		if iw := width - 2; iw > 0 && iw != u.treeWidth {
			u.treeWidth = iw
			go u.app.QueueUpdateDraw(func() {
				if u.treeWidth == iw {
					u.buildTree()
				}
			})
		}
		drawScrollbar(screen, x+width-1, y+1, height-2, u.tree.GetScrollOffset(), u.tree.GetRowCount(), height-2)
		return u.tree.GetInnerRect()
	})
	markFocus(u.tree,
		func() {
			u.lastPanel = u.tree
			u.dismissMenuIfOpen()
			u.status.SetText(u.treeHints())
			u.tree.SetTitle(focusTitle(titleTree))
		},
		func() { u.tree.SetTitle(titleTree) })
	u.tree.SetSelectedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if ref == nil {
			node.SetExpanded(!node.IsExpanded())
			return
		}
		d := ref.(*telegram.Dialog)
		if d.Forum && d.TopicID == 0 { // супергруппа-форум → раскрыть темы
			u.loadForum(node, *d)
			node.SetExpanded(!node.IsExpanded())
			return
		}
		u.openChat(*d)
		// Enter на чате/теме → прячем список чатов и уходим в сообщения.
		if u.showTree {
			u.showTree = false
			u.rebuildMid()
		}
		u.app.SetFocus(u.messages)
	})
	// ←/→ — свернуть/развернуть закладку (узел дерева). Esc возвращает к открытой
	// переписке; пока ни один чат не открыт, делать нечего — Esc игнорируется.
	// ↑↓ оставляем дереву.
	u.tree.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		cur := u.tree.GetCurrentNode()
		switch ev.Key() {
		case tcell.KeyEscape:
			if u.open != nil {
				u.showTree = false
				u.rebuildMid()
				u.app.SetFocus(u.messages)
			}
			return nil
		case tcell.KeyDelete: // Delete — убрать закладку из избранного
			u.removeBookmarkNode(cur)
			return nil
		case tcell.KeyRight:
			if cur != nil {
				if ref := cur.GetReference(); ref != nil {
					if d := ref.(*telegram.Dialog); d.Forum && d.TopicID == 0 {
						u.loadForum(cur, *d) // форум → подгрузить темы и раскрыть
						cur.SetExpanded(true)
						return nil
					}
				}
				if len(cur.GetChildren()) > 0 {
					cur.SetExpanded(true)
				}
			}
			return nil
		case tcell.KeyLeft:
			if cur == nil {
				return nil
			}
			if len(cur.GetChildren()) > 0 && cur.IsExpanded() {
				cur.SetExpanded(false) // развёрнутую закладку — свернуть
			} else if p := u.treeParent(cur); p != nil && p != u.tree.GetRoot() {
				p.SetExpanded(false) // из чата — свернуть его закладку и встать на неё
				u.tree.SetCurrentNode(p)
			}
			return nil
		}
		if ev.Rune() == 'd' { // d — убрать закладку из избранного (с подтверждением)
			u.removeBookmarkNode(cur)
			return nil
		}
		return ev
	})

	u.messages = newMsgList(u) // свой примитив ленты (фон на всю ширину, усечение, F3)
	u.msgTitle = " Сообщения "
	u.messages.SetBorder(true).SetTitle(u.msgTitle)
	markFocus(u.messages,
		func() {
			u.lastPanel = u.messages
			u.dismissMenuIfOpen()
			u.status.SetText(msgHints())
			u.messages.SetTitle(focusTitle(u.msgTitle))
		},
		func() { u.messages.SetTitle(u.msgTitle) })
	u.messages.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyUp:
			u.selectMsg(u.msgSel - 1)
			return nil
		case tcell.KeyDown:
			u.selectMsg(u.msgSel + 1)
			return nil
		case tcell.KeyHome:
			u.selectMsg(0)
			return nil
		case tcell.KeyEnd:
			u.selectMsg(len(u.history) - 1)
			return nil
		case tcell.KeyEnter: // Enter → панель ввода (если в чат можно писать)
			if u.open == nil || u.open.CanSend {
				u.app.SetFocus(u.input)
			}
			return nil
		case tcell.KeyEscape: // Esc → показать список чатов и вернуться в него
			if !u.showTree {
				u.showTree = true
				u.rebuildMid()
			}
			u.app.SetFocus(u.tree)
			return nil
		case tcell.KeyTab: // Tab/Shift+Tab → следующий/предыдущий элемент сообщения
			u.cycleElement(1)
			return nil
		case tcell.KeyBacktab:
			u.cycleElement(-1)
			return nil
		case tcell.KeyF3: // F3 → полный текст сообщения в отдельном окне
			if u.msgSel >= 0 && u.msgSel < len(u.history) {
				u.showMessageViewer(u.history[u.msgSel])
			}
			return nil
		}
		switch ev.Rune() {
		case 'c':
			u.elemCopy() // копировать: текст сообщения или адрес ссылки
			return nil
		case 'r':
			u.quoteMsg()
			return nil
		case 'd':
			u.deleteMsg()
			return nil
		case ' ':
			u.toggleSelect()
			return nil
		case 'y':
			u.copySelected()
			return nil
		case 'o':
			u.elemOpen() // открыть: ссылку в браузере или скачать вложение
			return nil
		}
		return ev
	})

	u.input = tview.NewTextArea()
	u.input.SetPlaceholder("Сообщение…  (Enter — отправить, Alt+Enter — перенос строки)")
	u.input.SetBorder(true).SetTitle(titleInput)
	markFocus(u.input,
		func() {
			u.lastPanel = u.input
			u.dismissMenuIfOpen()
			u.status.SetText(inputHints())
			u.input.SetTitle(focusTitle(titleInput))
		},
		func() { u.input.SetTitle(titleInput) })
	u.input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEnter && ev.Modifiers()&tcell.ModAlt == 0 {
			text := strings.TrimSpace(u.input.GetText())
			if text != "" && u.open != nil && u.open.CanSend {
				go u.sendMessage(*u.open, text)
			}
			return nil
		}
		if ev.Key() == tcell.KeyEscape { // Esc → обратно к сообщениям чата
			u.app.SetFocus(u.messages)
			return nil
		}
		if ev.Key() == tcell.KeyTab { // Tab не нужен в поле ввода (панельный Tab убран)
			return nil
		}
		return ev
	})

	u.details = tview.NewList().ShowSecondaryText(true)
	u.detailsTitle = " Информация "
	u.details.SetBorder(true).SetTitle(u.detailsTitle)
	u.details.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		off, _ := u.details.GetOffset()
		// Каждый элемент списка занимает 2 строки (есть вторичный текст),
		// поэтому видимая часть в «элементах» — половина высоты.
		drawScrollbar(screen, x+width-1, y+1, height-2, off, u.details.GetItemCount(), (height-2)/2)
		return u.details.GetInnerRect()
	})
	markFocus(u.details,
		func() {
			u.lastPanel = u.details
			u.dismissMenuIfOpen()
			u.status.SetText(detailsHints())
			u.details.SetTitle(focusTitle(u.detailsTitle))
		},
		func() { u.details.SetTitle(u.detailsTitle) })
	u.details.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape { // Esc → закрыть «Информацию», вернуться к переписке
			u.toggleInfo()
			return nil
		}
		if ev.Rune() == 'c' {
			i := u.details.GetCurrentItem()
			if i >= 0 && i < len(u.detailValues) {
				u.copyToClipboard(u.detailValues[i], "["+theme.Success+"]Скопировано[-]  "+detailsHints())
			}
			return nil
		}
		return ev
	})

	// Нижняя строка состояния в стиле Borland: серый фон, подсказки горячих
	// клавиш; каждая подсказка — кликабельный регион (см. runStatusAction).
	u.status = tview.NewTextView().SetDynamicColors(true).SetRegions(true)
	u.status.SetBackgroundColor(hex(theme.BarBg))
	u.status.SetText(statusHints())
	u.status.SetHighlightedFunc(func(added, _, _ []string) {
		if len(added) == 0 {
			return
		}
		id := added[0]
		u.status.Highlight() // снять подсветку сразу
		u.runStatusAction(id)
		if u.app.GetFocus() == u.status && u.lastPanel != nil {
			u.app.SetFocus(u.lastPanel) // клик не должен «забирать» фокус у панели
		}
	})

	// Заголовок окна терминала отражает открытый чат.
	u.app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		title := "tgcli"
		if u.open != nil {
			title = "tgcli — " + u.open.Title
		}
		s.SetTitle(title)
		return false
	})

	// Глобальные хоткеи.
	u.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		// При открытом модальном окне (диалог/просмотр/ввод) все клавиши идут
		// в него — глобальные хоткеи не вмешиваются.
		if u.pages != nil && (u.pages.HasPage("dialog") || u.pages.HasPage("viewer") || u.pages.HasPage("prompt")) {
			return ev
		}
		// Alt+буква: открыть пункт меню по «горячей» букве; Alt+X — выход.
		if ev.Key() == tcell.KeyRune && ev.Modifiers()&tcell.ModAlt != 0 {
			r := unicode.ToLower(ev.Rune())
			if r == 'x' || r == 'ч' { // 'ч' — клавиша X в русской раскладке
				u.app.Stop()
				return nil
			}
			if r == 'i' || r == 'ш' { // Alt+I — инфо-панель (ш — клавиша I в рус. раскладке)
				u.toggleInfo()
				return nil
			}
			for i, m := range u.menus() {
				if hot := []rune(m.title); len(hot) > 0 && unicode.ToLower(hot[0]) == r {
					u.openMenu(i)
					return nil
				}
			}
		}
		// Shift+F10 — локальное (контекстное) меню активной панели.
		if ev.Key() == tcell.KeyF10 && ev.Modifiers()&tcell.ModShift != 0 {
			u.openContextMenu()
			return nil
		}
		switch ev.Key() {
		case tcell.KeyCtrlC:
			u.app.Stop()
			return nil
		case tcell.KeyF10: // активировать/закрыть верхнее меню
			if u.menuActive >= 0 {
				u.closeMenu()
			} else {
				u.openMenu(0)
			}
			return nil
		case tcell.KeyF1:
			u.showHelp()
			return nil
		case tcell.KeyF8: // циклически сменить цветовую тему
			u.cycleTheme()
			return nil
		case tcell.KeyCtrlB:
			u.toggleTree()
			return nil
		case tcell.KeyCtrlD: // Ctrl+D — добавить выбранный чат в избранное
			u.bookmarkCurrent()
			return nil
		}
		return ev
	})
}

func (u *ui) root() *tview.Flex {
	u.center = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.messages, 0, 1, false).
		AddItem(u.input, 5, 0, false)

	u.mid = tview.NewFlex()
	u.showTree = true
	u.rebuildMid()

	u.header = tview.NewTextView().SetDynamicColors(true).SetRegions(true)
	u.header.SetBackgroundColor(tcell.GetColor(theme.BarBg))
	// Клик мышью по пункту бара tview сам превращает в Highlight(region) — здесь
	// открываем соответствующее меню. Программные смены подсветки (из openMenu/
	// dismissMenu) помечены menuGuard и игнорируются, чтобы не было рекурсии.
	// Закрытие меню НЕ идёт через этот колбэк: иначе app.SetFocus мог бы быть
	// вызван из Blur под мьютексом приложения (deadlock).
	u.header.SetHighlightedFunc(func(added, _, _ []string) {
		if u.menuGuard || len(added) == 0 {
			return
		}
		if i, err := strconv.Atoi(strings.TrimPrefix(added[0], "t")); err == nil {
			u.openMenu(i)
		}
	})
	u.renderMenuBar()

	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.header, 1, 0, false).
		AddItem(u.mid, 0, 1, false).
		AddItem(u.status, 1, 0, false)
}

// rebuildMid пересобирает среднюю строку. Панели «Чаты» и «Сообщения» —
// взаимоисключающие полноэкранные виды: открытый список чатов занимает всю
// ширину, переписка скрыта (и наоборот). «Информация» приклеивается справа к
// переписке.
func (u *ui) rebuildMid() {
	u.mid.Clear()
	if u.showTree {
		u.mid.AddItem(u.tree, 0, 1, false)
		return
	}
	u.mid.AddItem(u.center, 0, 1, false)
	if u.showDetails {
		u.mid.AddItem(u.details, 34, 0, false)
	}
}

// ── Фокус и панели ─────────────────────────────────────────────────────────

// focusOrder — порядок обхода панелей по Tab (зависит от видимых панелей).
func (u *ui) focusOrder() []tview.Primitive {
	order := []tview.Primitive{u.tree, u.messages}
	if u.open == nil || u.open.CanSend {
		order = append(order, u.input)
	}
	if u.showDetails {
		order = append(order, u.details)
	}
	return order
}

func (u *ui) cycleFocus()     { u.cycleFocusStep(1) }
func (u *ui) cycleFocusBack() { u.cycleFocusStep(-1) }

func (u *ui) cycleFocusStep(step int) {
	order := u.focusOrder()
	cur := u.app.GetFocus()
	for i, p := range order {
		if p == cur {
			u.app.SetFocus(order[(i+step+len(order))%len(order)])
			return
		}
	}
	u.app.SetFocus(u.tree)
}

func (u *ui) toggleTree() {
	if u.showTree && u.open == nil {
		return // переписка ещё не открыта — переключаться не на что
	}
	u.showTree = !u.showTree
	u.rebuildMid()
	if u.showTree {
		u.app.SetFocus(u.tree)
	} else {
		u.app.SetFocus(u.messages)
	}
}

func (u *ui) toggleDetails() {
	u.showDetails = !u.showDetails
	if u.showDetails {
		u.renderDetails()
	}
	u.rebuildMid()
	if !u.showDetails && u.app.GetFocus() == u.details {
		u.app.SetFocus(u.tree)
	}
}

// toggleInfo открывает/закрывает панель «Информация» (Alt+I). Информация
// относится к открытой переписке, поэтому показывается рядом с ней (вид «Чаты»
// при этом сменяется на переписку) и недоступна, пока чат не открыт.
func (u *ui) toggleInfo() {
	if u.open == nil {
		return // нет открытого чата — нечего показывать
	}
	u.showDetails = !u.showDetails
	if u.showDetails {
		u.showTree = false
		u.renderDetails()
		u.rebuildMid()
		u.app.SetFocus(u.details)
		return
	}
	u.rebuildMid()
	u.app.SetFocus(u.messages)
}

// ── Темы ───────────────────────────────────────────────────────────────────

// cycleTheme переключает на следующую тему из списka themes (F8).
func (u *ui) cycleTheme() {
	next := 0
	for i, t := range themes {
		if t.Name == theme.Name {
			next = (i + 1) % len(themes)
			break
		}
	}
	u.setTheme(themes[next])
}

// setTheme делает t активной: обновляет палитру, перекрашивает уже созданные
// виджеты, сохраняет выбор в конфиг и перерисовывает экран.
func (u *ui) setTheme(t Theme) {
	if t.Name == theme.Name {
		return
	}
	u.dismissMenuIfOpen() // выпадающее меню перерисовывать ни к чему — закрываем
	applyTheme(t)
	u.restyle()
	u.saveTheme(t.Name)
	u.status.SetText("[" + theme.Success + "]Тема: " + t.Name + "[-]  " + statusHints())
}

// restyle переустанавливает цвета виджетов, захваченные из tview.Styles при их
// создании (одной смены глобальной палитры для них мало). Примитивы, рисующие
// себя сами (лента, диалоги, выпадающие меню), подхватят тему сами при
// ближайшей перерисовке.
func (u *ui) restyle() {
	bg := tcell.GetColor(theme.BgPanel)
	for _, b := range []focusBox{u.tree, u.input, u.details, u.messages} {
		b.SetBackgroundColor(bg)
		if b.HasFocus() { // рамка/заголовок — как в markFocus, но без смены фокуса
			b.SetBorderColor(tcell.GetColor(theme.BorderActive))
			b.SetTitleColor(tcell.GetColor(theme.TitleActive))
		} else {
			b.SetBorderColor(tcell.GetColor(theme.Inactive))
			b.SetTitleColor(tcell.GetColor(theme.Inactive))
		}
	}

	u.input.SetTextStyle(tcell.StyleDefault.Background(bg).Foreground(tcell.GetColor(theme.Text)))
	u.input.SetPlaceholderStyle(tcell.StyleDefault.Background(bg).Foreground(tcell.GetColor(theme.TextDim)))
	u.details.SetMainTextColor(tcell.GetColor(theme.Text)).
		SetSecondaryTextColor(tcell.GetColor(theme.TextDim))

	barBg := tcell.GetColor(theme.BarBg)
	u.status.SetBackgroundColor(barBg)
	u.header.SetBackgroundColor(barBg)
	u.renderMenuBar() // акселераторы бара берут цвета из темы
	u.recolorTree()   // перекрасить узлы дерева, не теряя раскрытие/выбор
	// Перерисовку делает событийный цикл tview после обработчика (как в toggleTree).
}

// recolorTree перекрашивает узлы дерева под активную тему на месте — в отличие
// от buildTree не сбрасывает раскрытие закладок и текущий выбор. Цвет узла-чата
// берётся из его ссылки (Dialog.Kind), категории — по началу подписи.
func (u *ui) recolorTree() {
	root := u.tree.GetRoot()
	if root == nil {
		return
	}
	root.SetColor(tcell.GetColor(theme.TextBright))
	catColor := map[string]string{
		"★ Избранное":     theme.Warn,
		"● Непрочитанные": kindColor("unread"),
	}
	for _, g := range kindOrder {
		catColor[strings.TrimSpace(g.label)] = kindColor(g.key)
	}
	root.Walk(func(n, parent *tview.TreeNode) bool {
		if n == root {
			return true
		}
		if d, ok := n.GetReference().(*telegram.Dialog); ok { // чат, тема форума, Saved Messages
			n.SetColor(tcell.GetColor(kindColor(d.Kind)))
			return true
		}
		if parent == root { // категория-закладка
			text := strings.TrimSpace(n.GetText())
			for label, color := range catColor {
				if strings.HasPrefix(text, label) {
					n.SetColor(tcell.GetColor(color))
					return true
				}
			}
		}
		n.SetColor(tcell.GetColor(theme.Inactive)) // заглушки «…», «(нет тем)»
		return true
	})
}

// saveTheme запоминает выбранную тему в конфиге (в фоне, чтобы не блокировать UI).
func (u *ui) saveTheme(name string) {
	if u.cfg == nil {
		return
	}
	u.cfg.Theme = name
	go func() { _ = u.cfg.Save() }()
}

// ── Данные ─────────────────────────────────────────────────────────────────

func (u *ui) loadDialogs() {
	if u.cache != nil {
		if d, err := u.cache.Dialogs(); err == nil && len(d) > 0 {
			u.app.QueueUpdateDraw(func() { u.setDialogs(d) })
		}
	}
	d, err := u.sess.Dialogs(u.ctx, 200, false)
	if err != nil {
		u.app.QueueUpdateDraw(func() {
			u.status.SetText("[" + theme.ErrorC + "]Ошибка диалогов: " + tview.Escape(err.Error()))
		})
		return
	}
	if u.cache != nil {
		_ = u.cache.SaveDialogs(d)
	}
	u.app.QueueUpdateDraw(func() { u.setDialogs(d) })
}

func (u *ui) setDialogs(d []telegram.Dialog) {
	// Дедуп по ключу чата: иногда список диалогов содержит один и тот же чат
	// дважды (перекрытие страниц/папки) — оставляем первое вхождение.
	seen := make(map[string]bool, len(d))
	uniq := make([]telegram.Dialog, 0, len(d))
	for _, x := range d {
		k := x.Ref.Key()
		if seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, x)
	}
	u.dialogs = uniq
	u.buildTree()
}

// treeAvail — свободная ширина под текст узла на уровне level. TreeView рисует
// текст со смещением textX = 3·level (graphics + отступ 2 на уровень), поэтому
// доступная ширина = внутренняя ширина панели минус это смещение. Ширина панели
// динамическая (≈половина экрана) — берётся из u.treeWidth, который обновляется
// при отрисовке дерева.
func (u *ui) treeAvail(level int) int {
	w := u.treeWidth - 3*level - 1 // −1 — зазор перед скроллбаром
	if w < 4 {
		w = 4
	}
	return w
}

// treeLine форматирует строку узла: заголовок слева, счётчик прижат к правому
// краю, между ними — заполнение пробелами до доступной ширины width. Ширина
// считается в КЛЕТКАХ терминала (эмодзи/CJK = 2), длинный заголовок усекается
// многоточием — иначе широкие символы перекашивают выравнивание.
func treeLine(title, count string, width int) string {
	title = strings.TrimSpace(title)
	if count == "" {
		return runewidth.Truncate(title, width, "…")
	}
	cw := runewidth.StringWidth(count)
	maxTitle := width - cw - 1
	if maxTitle < 1 {
		maxTitle = 1
	}
	title = runewidth.Truncate(title, maxTitle, "…")
	gap := width - runewidth.StringWidth(title) - cw
	if gap < 1 {
		gap = 1
	}
	return title + strings.Repeat(" ", gap) + count
}

// Ширины колонок строки чата (в клетках терминала).
const (
	nickColW  = 18 // колонка «@ник»
	countColW = 6  // колонка счётчика непрочитанных, прижата вправо
)

// treeRow форматирует строку чата тремя колонками: имя (слева, тянется), @ник
// (фиксированная колонка), счётчик непрочитанных (справа). На узкой панели
// деградирует до «имя … счётчик», чтобы не ломать выравнивание.
func treeRow(name, nick, count string, width int) string {
	if width < nickColW+countColW+8 {
		full := strings.TrimSpace(name)
		if nick != "" {
			full += "  " + nick
		}
		return treeLine(full, count, width)
	}
	nameW := width - nickColW - countColW
	name = padRightCells(runewidth.Truncate(strings.TrimSpace(name), nameW, "…"), nameW)
	nick = padRightCells(runewidth.Truncate(nick, nickColW, "…"), nickColW)
	count = padLeftCells(runewidth.Truncate(count, countColW, ""), countColW)
	return name + nick + count
}

// splitTitle разбивает «@ник - Имя Фамилия» (формат DisplayName) на имя и @ник.
// Без «@» весь заголовок считается именем (группы/каналы без username).
func splitTitle(title string) (name, nick string) {
	title = strings.TrimSpace(title)
	if strings.HasPrefix(title, "@") {
		if i := strings.Index(title, " - "); i >= 0 {
			return strings.TrimSpace(title[i+3:]), strings.TrimSpace(title[:i])
		}
		return "", title // только @ник
	}
	return title, ""
}

// padRightCells/padLeftCells дополняют строку пробелами до ширины w в клетках.
func padRightCells(s string, w int) string {
	if g := w - runewidth.StringWidth(s); g > 0 {
		return s + strings.Repeat(" ", g)
	}
	return s
}

func padLeftCells(s string, w int) string {
	if g := w - runewidth.StringWidth(s); g > 0 {
		return strings.Repeat(" ", g) + s
	}
	return s
}

// treeParent находит родителя узла (TreeNode не хранит ссылку на родителя).
func (u *ui) treeParent(target *tview.TreeNode) *tview.TreeNode {
	var found *tview.TreeNode
	if root := u.tree.GetRoot(); root != nil {
		root.Walk(func(n, parent *tview.TreeNode) bool {
			if n == target {
				found = parent
				return false
			}
			return true
		})
	}
	return found
}

// loadForum один раз подгружает темы форум-супергруппы (асинхронно) и заменяет
// ими заглушку-«…» под её узлом. Закрытые темы помечаются 🔒 и доступны только
// для чтения.
func (u *ui) loadForum(node *tview.TreeNode, d telegram.Dialog) {
	if u.forumLoaded == nil {
		u.forumLoaded = map[string]bool{}
	}
	if u.forumLoaded[d.Ref.Key()] || u.sess == nil {
		return
	}
	u.forumLoaded[d.Ref.Key()] = true
	col := tcell.GetColor(kindColor("supergroup"))
	go func() {
		topics, err := u.sess.ForumTopics(u.ctx, d.Peer, 100)
		u.app.QueueUpdateDraw(func() {
			node.ClearChildren()
			if err != nil {
				node.AddChild(tview.NewTreeNode("  ошибка: " + tview.Escape(err.Error())).SetSelectable(false))
				u.forumLoaded[d.Ref.Key()] = false // дать повторить позже
				return
			}
			if len(topics) == 0 {
				node.AddChild(tview.NewTreeNode("  (нет тем)").SetSelectable(false).
					SetColor(tcell.GetColor(theme.Inactive)))
				return
			}
			for _, t := range topics {
				title := t.Title
				if t.Closed {
					title = "🔒 " + title
				}
				td := d
				td.TopicID = t.ID
				td.TopicTitle = t.Title
				td.CanSend = d.CanSend && !t.Closed
				node.AddChild(tview.NewTreeNode(treeLine(title, "", u.treeAvail(3))).
					SetReference(&td).SetColor(col))
			}
		})
	}()
}

func (u *ui) buildTree() {
	// Узлы пересоздаются — сбрасываем отметки загруженных форумов, иначе заглушка
	// «…» залипнет (loadForum посчитает темы уже загруженными).
	u.forumLoaded = map[string]bool{}
	u.favNode = nil
	groups := map[string][]telegram.Dialog{}
	for _, d := range u.dialogs {
		groups[groupKey(d)] = append(groups[groupKey(d)], d)
	}
	root := tview.NewTreeNode("Закладки").SetSelectable(false).
		SetColor(tcell.GetColor(theme.TextBright))

	// ★ Избранное — локальные закладки (свои имена, хранятся в конфиге).
	if u.cfg != nil && len(u.cfg.Bookmarks) > 0 {
		cat := tview.NewTreeNode(treeLine("★ Избранное", fmt.Sprintf("(%d)", len(u.cfg.Bookmarks)), u.treeAvail(1))).
			SetColor(tcell.GetColor(theme.Warn)).SetSelectable(true).SetExpanded(true)
		for _, b := range u.cfg.Bookmarks {
			cat.AddChild(u.chatNode(u.bookmarkDialog(b)))
		}
		root.AddChild(cat)
		u.favNode = cat
	}

	// ● Непрочитанные — сводная закладка: чаты с непрочитанными, не в муте.
	var unread []telegram.Dialog
	for _, d := range u.dialogs {
		if d.Unread > 0 && !d.Muted {
			unread = append(unread, d)
		}
	}
	if len(unread) > 0 {
		cat := tview.NewTreeNode(treeLine("● Непрочитанные", fmt.Sprintf("(%d)", len(unread)), u.treeAvail(1))).
			SetColor(tcell.GetColor(kindColor("unread"))).SetSelectable(true).SetExpanded(true)
		for i := range unread {
			cat.AddChild(u.chatNode(unread[i]))
		}
		root.AddChild(cat)
	}

	// 💾 Saved Messages — отдельной строкой верхнего уровня (а не категорией).
	for i := range u.dialogs {
		if u.dialogs[i].Kind == "self" {
			sd := u.dialogs[i]
			count := ""
			if sd.Unread > 0 {
				count = fmt.Sprintf("(%d)", sd.Unread)
			}
			root.AddChild(tview.NewTreeNode(treeRow("💾 Saved Messages", "", count, u.treeAvail(1))).
				SetReference(&sd).SetColor(tcell.GetColor(kindColor("self"))))
			break
		}
	}

	for _, g := range kindOrder {
		list := groups[g.key]
		if len(list) == 0 {
			continue
		}
		cat := tview.NewTreeNode(treeLine(g.label, fmt.Sprintf("(%d)", len(list)), u.treeAvail(1))).
			SetColor(tcell.GetColor(kindColor(g.key))).SetSelectable(true)
		for i := range list {
			cat.AddChild(u.chatNode(list[i]))
		}
		root.AddChild(cat)
	}
	u.tree.SetRoot(root).SetCurrentNode(root)
}

// chatNode строит узел-лист чата второго уровня: цвет по типу, счётчик
// непрочитанных справа; форум получает заглушку «…» для ленивой подгрузки тем.
func (u *ui) chatNode(d telegram.Dialog) *tview.TreeNode {
	count := ""
	if d.Unread > 0 {
		count = fmt.Sprintf("(%d)", d.Unread)
	}
	name, nick := splitTitle(d.Title)
	col := tcell.GetColor(kindColor(d.Kind))
	node := tview.NewTreeNode(treeRow(name, nick, count, u.treeAvail(2))).
		SetReference(&d).SetColor(col)
	if d.Forum {
		node.SetExpanded(false)
		node.AddChild(tview.NewTreeNode("  …").SetSelectable(false).
			SetColor(tcell.GetColor(theme.Inactive)))
	}
	return node
}

func (u *ui) openChat(d telegram.Dialog) {
	dd := d
	u.open = &dd
	u.history = nil
	u.msgSel = -1
	u.msgElem = 0
	u.messages.offset = 0
	u.selected = map[int]bool{}
	u.input.SetDisabled(!dd.CanSend)
	if dd.CanSend {
		u.input.SetPlaceholder("Сообщение…  (Enter — отправить, Alt+Enter — перенос строки)")
	} else {
		u.input.SetPlaceholder("Только чтение — нет прав на отправку в этом чате")
	}
	if dd.TopicID != 0 {
		u.msgTitle = " " + tview.Escape(dd.Title) + " / " + tview.Escape(dd.TopicTitle) + " "
	} else {
		u.msgTitle = " " + tview.Escape(dd.Title) + " "
	}
	u.setTitle(u.messages, u.msgTitle)
	u.messages.placeholder = "Загрузка истории…"
	if u.showDetails {
		u.renderDetails()
	}

	go func() {
		// История темы форума загружается тредом ответов; кеш — только для
		// обычных чатов (тема перезапрашивается всегда).
		if dd.TopicID == 0 && u.cache != nil {
			if h, err := u.cache.History(dd.Ref.Key()); err == nil && len(h) > 0 {
				u.app.QueueUpdateDraw(func() {
					if u.sameChat(dd) {
						u.setHistory(h)
					}
				})
			}
		}
		var (
			h   []telegram.HistoryMessage
			err error
		)
		if dd.TopicID != 0 {
			h, err = u.sess.HistoryByTopic(u.ctx, dd.Peer, dd.TopicID, 60)
		} else {
			h, err = u.sess.HistoryByPeer(u.ctx, dd.Peer, 60)
			if err == nil && u.cache != nil {
				_ = u.cache.SaveHistory(dd.Ref.Key(), h)
			}
		}
		u.app.QueueUpdateDraw(func() {
			if !u.sameChat(dd) {
				return
			}
			if err != nil {
				u.messages.placeholder = "Ошибка загрузки истории"
				u.status.SetText("[" + theme.ErrorC + "]Ошибка: " + tview.Escape(err.Error()) + "[-]")
				return
			}
			u.setHistory(h)
		})
	}()
}

// sameChat сообщает, открыт ли сейчас именно этот чат/тема.
func (u *ui) sameChat(d telegram.Dialog) bool {
	return u.open != nil && u.open.Ref.Key() == d.Ref.Key() && u.open.TopicID == d.TopicID
}

func (u *ui) sendMessage(d telegram.Dialog, text string) {
	var err error
	if d.TopicID != 0 {
		_, err = u.sess.SendToTopic(u.ctx, d.Peer, d.TopicID, text)
	} else {
		_, err = u.sess.SendToPeer(u.ctx, d.Peer, text)
	}
	u.app.QueueUpdateDraw(func() {
		if err != nil {
			u.status.SetText("[" + theme.ErrorC + "]Ошибка отправки: " + tview.Escape(err.Error()))
			return
		}
		u.input.SetText("", false)
		u.status.SetText(inputHints())
	})
	if err == nil {
		go u.reloadHistory(d)
	}
}

func (u *ui) listenUpdates(updates <-chan telegram.NewMessage) {
	for nm := range updates {
		nm := nm
		u.app.QueueUpdateDraw(func() {
			if u.open != nil && u.open.TopicID == 0 && nm.PeerKey == u.open.Ref.Key() {
				u.history = append(u.history, nm.Message)
			} else if !nm.Message.Out {
				u.status.SetText(" [" + theme.Accent + "::b]● НОВОЕ[-:-:-] " + statusHints())
			}
		})
	}
}

// ── Рендер ─────────────────────────────────────────────────────────────────

// setHistory ставит историю и выставляет курсор на последнее сообщение, если он
// вышел за пределы (после загрузки/перезагрузки чата). Отрисовку делает примитив
// messages при следующем Draw.
func (u *ui) setHistory(h []telegram.HistoryMessage) {
	u.history = h
	if u.msgSel < 0 || u.msgSel >= len(h) {
		u.msgSel = len(h) - 1
	}
}

// showMessageViewer открывает полный текст сообщения в отдельном скроллируемом
// окне (F3); закрытие — Esc.
func (u *ui) showMessageViewer(msg telegram.HistoryMessage) {
	text := msg.Plain()
	if msg.Media != nil {
		if text != "" {
			text += "\n\n"
		}
		text += "📎 " + msg.Media.Label()
	}
	title := msg.Author
	if msg.Out {
		title = "Вы"
	}
	tv := tview.NewTextView().SetWrap(true).SetText(text)
	tv.SetBorder(true).SetTitle(" " + tview.Escape(title) + " — Esc закрыть ")
	tv.SetBorderColor(tcell.GetColor(theme.BorderActive))
	tv.SetTitleColor(tcell.GetColor(theme.TitleActive))
	tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			u.pages.RemovePage("viewer")
			u.app.SetFocus(u.messages)
			return nil
		}
		return ev
	})
	u.pages.AddPage("viewer", tv, true, true)
	u.app.SetFocus(tv)
}

func (u *ui) renderDetails() {
	u.details.Clear()
	u.detailValues = nil
	if u.open == nil {
		return
	}
	add := func(label, value string) {
		u.details.AddItem(label, value, 0, nil)
		u.detailValues = append(u.detailValues, value)
	}
	add("Имя", u.open.Title)
	add("Тип", u.open.Kind)
	add("ID", u.open.Ref.Key())
	if u.open.Unread > 0 {
		add("Непрочитано", fmt.Sprint(u.open.Unread))
	}
	u.detailsTitle = " Информация — c копировать "
	u.setTitle(u.details, u.detailsTitle)
}

// ── Действия над сообщениями ───────────────────────────────────────────────

func (u *ui) selectMsg(i int) {
	if len(u.history) == 0 {
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(u.history) {
		i = len(u.history) - 1
	}
	u.msgSel = i
	u.msgElem = 0 // при смене сообщения навигация по элементам — с начала
	u.showElementHints()
	// Отрисовку (полосу-курсор и прокрутку к выбранному) сделает примитов messages.
}

func (u *ui) selectedIndices() []int {
	var idx []int
	for i := 0; i < len(u.history); i++ {
		if u.selected[i] {
			idx = append(idx, i)
		}
	}
	return idx
}

// copyToClipboard кладёт текст в буфер обмена в фоне. Вызов xclip/xsel/wl-copy
// может подвиснуть, поэтому его нельзя выполнять в событийном цикле (иначе фриз).
func (u *ui) copyToClipboard(text, okStatus string) {
	go func() {
		err := clipboard.WriteAll(text)
		u.app.QueueUpdateDraw(func() {
			if err != nil {
				u.status.SetText("[" + theme.ErrorC + "]Буфер недоступен (нужен xclip/xsel/wl-clipboard)[-]")
			} else {
				u.status.SetText(okStatus)
			}
		})
	}()
}

func (u *ui) copyMsg() {
	if u.msgSel < 0 || u.msgSel >= len(u.history) {
		return
	}
	u.copyToClipboard(u.history[u.msgSel].Plain(), "["+theme.Success+"]Скопировано[-]  "+msgHints())
}

func (u *ui) copySelected() {
	idx := u.selectedIndices()
	if len(idx) == 0 {
		u.copyMsg()
		return
	}
	var parts []string
	for _, i := range idx {
		parts = append(parts, u.history[i].Plain())
	}
	u.copyToClipboard(strings.Join(parts, "\n\n"),
		fmt.Sprintf("[%s]Скопировано сообщений: %d[-]  %s", theme.Success, len(idx), msgHints()))
}

func (u *ui) quoteMsg() {
	if u.msgSel < 0 || u.msgSel >= len(u.history) {
		return
	}
	var b strings.Builder
	if cur := u.input.GetText(); cur != "" {
		b.WriteString(cur)
		if !strings.HasSuffix(cur, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, line := range strings.Split(u.history[u.msgSel].Plain(), "\n") {
		b.WriteString("> " + line + "\n")
	}
	u.input.SetText(b.String(), true)
	u.app.SetFocus(u.input)
}

func (u *ui) toggleSelect() {
	if u.msgSel < 0 || u.msgSel >= len(u.history) {
		return
	}
	if u.selected[u.msgSel] {
		delete(u.selected, u.msgSel)
	} else {
		u.selected[u.msgSel] = true
	}
}

// msgElement — навигационный элемент сообщения (Tab): текст, ссылка, вложение.
type msgElement struct {
	kind  string // text, link, media
	value string // для link — URL
	label string
}

// messageElements собирает элементы сообщения i для навигации по Tab.
func (u *ui) messageElements(i int) []msgElement {
	if i < 0 || i >= len(u.history) {
		return nil
	}
	msg := u.history[i]
	var els []msgElement
	if strings.TrimSpace(msg.Plain()) != "" {
		els = append(els, msgElement{kind: "text", label: "текст"})
	}
	seen := map[string]bool{}
	for _, s := range msg.Spans {
		if s.URL != "" && !seen[s.URL] {
			seen[s.URL] = true
			els = append(els, msgElement{kind: "link", value: s.URL, label: s.URL})
		}
	}
	if msg.Media != nil {
		els = append(els, msgElement{kind: "media", label: msg.Media.Label()})
	}
	return els
}

func (u *ui) currentElement() (msgElement, bool) {
	els := u.messageElements(u.msgSel)
	if u.msgElem < 0 || u.msgElem >= len(els) {
		return msgElement{}, false
	}
	return els[u.msgElem], true
}

// cycleElement переключает текущий элемент сообщения и обновляет подсказки.
func (u *ui) cycleElement(step int) {
	els := u.messageElements(u.msgSel)
	if len(els) == 0 {
		return
	}
	u.msgElem = (u.msgElem + step + len(els)) % len(els)
	u.showElementHints()
}

// showElementHints показывает в строке состояния текущий элемент и его действия.
func (u *ui) showElementHints() {
	els := u.messageElements(u.msgSel)
	if len(els) <= 1 {
		u.status.SetText(msgHints())
		return
	}
	pos := fmt.Sprintf("[%d/%d] ", u.msgElem+1, len(els))
	switch e := els[u.msgElem]; e.kind {
	case "link":
		u.status.SetText(fmt.Sprintf("[%s]%s🔗 %s[-]  ", theme.MsgLink, pos, tview.Escape(e.value)) +
			borlandBar([][3]string{{"open", "o", "Браузер"}, {"copy", "c", "Копир.ссылку"}, {"menu", "F10", "Меню"}}))
	case "media":
		u.status.SetText(fmt.Sprintf("[%s]%s📎 %s[-]  ", theme.MsgCode, pos, tview.Escape(e.label)) +
			borlandBar([][3]string{{"open", "o", "Скачать/Открыть"}, {"menu", "F10", "Меню"}}))
	default:
		u.status.SetText(pos + msgHints())
	}
}

// elemCopy копирует адрес ссылки (если выбрана ссылка) или текст сообщения.
func (u *ui) elemCopy() {
	if e, ok := u.currentElement(); ok && e.kind == "link" {
		u.copyToClipboard(e.value, "["+theme.Success+"]Ссылка скопирована[-]  "+msgHints())
		return
	}
	u.copyMsg()
}

// elemOpen открывает ссылку в браузере или скачивает/открывает вложение.
func (u *ui) elemOpen() {
	if e, ok := u.currentElement(); ok && e.kind == "link" {
		if err := openExternal(e.value); err != nil {
			u.status.SetText("[" + theme.ErrorC + "]Не удалось открыть ссылку[-]")
		} else {
			u.status.SetText("[" + theme.Success + "]Открыто: " + tview.Escape(e.value) + "[-]  " + msgHints())
		}
		return
	}
	u.openAttachment()
}

// download — активная загрузка вложения: отмена (для паузы) и прогресс.
type download struct {
	cancel      context.CancelFunc
	done, total int64
}

// openAttachment по клавише o управляет вложением выбранного сообщения:
//   - уже скачано (есть в кеше) → открыть внешней программой;
//   - идёт загрузка → поставить на паузу (отменить, .part сохраняется);
//   - не качается → начать/докачать в фоне с прогрессом, по завершении открыть.
func (u *ui) openAttachment() {
	if u.msgSel < 0 || u.msgSel >= len(u.history) {
		return
	}
	msg := u.history[u.msgSel]
	if msg.Media == nil {
		u.status.SetText("[" + theme.Warn + "]У этого сообщения нет вложения[-]  " + msgHints())
		return
	}
	if u.open == nil || u.sess == nil {
		return
	}
	id := msg.ID
	path := u.attachmentPath(id, msg.Media)
	name := filepath.Base(path)

	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 { // уже в кеше
		u.openCached(path)
		return
	}
	if dl := u.downloads[id]; dl != nil { // уже качается → пауза
		dl.cancel()
		delete(u.downloads, id)
		u.status.SetText("[" + theme.Warn + "]⏸ Пауза: " + tview.Escape(name) + " (o — продолжить)[-]")
		return
	}

	ctx, cancel := context.WithCancel(u.ctx)
	u.downloads[id] = &download{cancel: cancel, total: msg.Media.Size}
	open := *u.open
	u.status.SetText("[" + theme.Info + "]⬇ Загрузка " + tview.Escape(name) + "…[-]")
	go func() {
		err := u.sess.DownloadMediaTo(ctx, open.Peer, id, path, func(done, total int64) {
			u.app.QueueUpdateDraw(func() {
				if dl := u.downloads[id]; dl != nil {
					dl.done, dl.total = done, total
					u.showDownloadProgress(name, done, total)
				}
			})
		})
		u.app.QueueUpdateDraw(func() {
			delete(u.downloads, id)
			switch {
			case errors.Is(err, context.Canceled):
				// пауза — статус уже показан, ничего не делаем
			case err != nil:
				u.status.SetText("[" + theme.ErrorC + "]Ошибка загрузки: " + tview.Escape(err.Error()) + "[-]")
			default:
				u.openCached(path)
			}
		})
	}()
}

// openCached открывает уже скачанный файл внешней программой.
func (u *ui) openCached(path string) {
	if err := openExternal(path); err != nil {
		u.status.SetText("[" + theme.Warn + "]Сохранено: " + tview.Escape(path) + " (открыть не удалось)[-]")
		return
	}
	u.status.SetText("[" + theme.Success + "]Открыто: " + tview.Escape(path) + "[-]  " + msgHints())
}

// showDownloadProgress показывает прогресс загрузки в строке состояния.
func (u *ui) showDownloadProgress(name string, done, total int64) {
	if total > 0 {
		u.status.SetText(fmt.Sprintf("[%s]⬇ %s %d%% (%s / %s)[-]  o — пауза",
			theme.Info, tview.Escape(name), done*100/total, humanBytes(done), humanBytes(total)))
	} else {
		u.status.SetText(fmt.Sprintf("[%s]⬇ %s %s[-]  o — пауза", theme.Info, tview.Escape(name), humanBytes(done)))
	}
}

// attachmentPath — путь к кешу вложения: <cache>/tgcli/media/<чат>/<id>_<имя>.
func (u *ui) attachmentPath(msgID int64, m *telegram.Media) string {
	name := m.FileName
	if name == "" {
		switch m.Kind {
		case "photo":
			name = "photo.jpg"
		case "voice":
			name = "voice.ogg"
		case "video", "gif", "round":
			name = m.Kind + ".mp4"
		default:
			name = m.Kind
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	peer := "chat"
	if u.open != nil {
		peer = strings.NewReplacer("/", "_", ":", "_").Replace(u.open.Ref.Key())
	}
	return filepath.Join(base, "tgcli", "media", peer,
		fmt.Sprintf("%d_%s", msgID, filepath.Base(name)))
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d Б", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), []string{"КБ", "МБ", "ГБ", "ТБ"}[exp])
}

// openExternal открывает файл системной программой по умолчанию (Linux: xdg-open).
func openExternal(path string) error {
	cmd := exec.Command("xdg-open", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (u *ui) deleteMsg() {
	idx := u.selectedIndices()
	if len(idx) == 0 && u.msgSel >= 0 && u.msgSel < len(u.history) {
		idx = []int{u.msgSel}
	}
	if len(idx) == 0 || u.open == nil {
		return
	}
	ids := make([]int, 0, len(idx))
	for _, i := range idx {
		ids = append(ids, int(u.history[i].ID))
	}
	open := *u.open
	u.confirm(fmt.Sprintf("Удалить сообщений: %d? Удаление у всех участников, необратимо.", len(ids)), func() {
		go func() {
			err := u.sess.DeleteMessages(u.ctx, open.Peer, ids)
			u.app.QueueUpdateDraw(func() {
				if err != nil {
					u.status.SetText("[" + theme.ErrorC + "]Ошибка удаления: " + tview.Escape(err.Error()) + "[-]")
					return
				}
				u.selected = map[int]bool{}
				u.status.SetText("[" + theme.Success + "]Удалено[-]  " + msgHints())
			})
			go u.reloadHistory(open)
		}()
	})
}

func (u *ui) reloadHistory(d telegram.Dialog) {
	var (
		h   []telegram.HistoryMessage
		err error
	)
	if d.TopicID != 0 {
		h, err = u.sess.HistoryByTopic(u.ctx, d.Peer, d.TopicID, 60)
	} else {
		h, err = u.sess.HistoryByPeer(u.ctx, d.Peer, 60)
	}
	if err != nil {
		return
	}
	if d.TopicID == 0 && u.cache != nil {
		_ = u.cache.SaveHistory(d.Ref.Key(), h)
	}
	u.app.QueueUpdateDraw(func() {
		if u.sameChat(d) {
			u.setHistory(h)
		}
	})
}

// showHelp показывает окно справки по горячим клавишам.
func (u *ui) showHelp() {
	help := "Чаты: ←→ свернуть/развернуть, Enter — открыть (список прячется)\n" +
		"Ctrl+B — список чатов   Alt+I — панель информации\n" +
		"\n" +
		"Сообщения:\n" +
		"↑↓/Home/End — выбор сообщения   Esc — назад к чатам\n" +
		"Tab/Shift+Tab — элемент сообщения (текст/ссылка/вложение)\n" +
		"c — копировать   r — цитировать   d — удалить   Spc — пометить\n" +
		"o — открыть: ссылку в браузере / скачать вложение (повторно — пауза)\n" +
		"F3 — полный текст сообщения   Enter — перейти к вводу\n" +
		"\n" +
		"Ввод: Enter — отправить, Alt+Enter — перенос, Esc — к сообщениям\n" +
		"F1 — справка   F8 — тема   F10 — меню   Alt+X / Ctrl+C — выход"
	u.showDialog("Горячие клавиши", help, []string{"OK"}, nil)
}

// confirm показывает модальное окно подтверждения. onYes вызывается только при
// выборе «Да» (кнопка с индексом 1).
func (u *ui) confirm(text string, onYes func()) {
	u.showDialog("Подтверждение", text, []string{"Отмена", "Да"}, func(idx int) {
		if idx == 1 {
			onYes()
		}
	})
}

// drawScrollbar рисует вертикальный скроллбар Turbo Vision на колонке col,
// строки y..y+h-1: ▲ сверху, ▼ снизу, между — дорожка ░ и ползунок █. Единицы
// offset/total/viewport должны совпадать (строки или элементы — неважно).
func drawScrollbar(screen tcell.Screen, col, y, h, offset, total, viewport int) {
	if h < 2 || viewport < 1 {
		return
	}
	style := tcell.StyleDefault.
		Background(tview.Styles.PrimitiveBackgroundColor).
		Foreground(tcell.GetColor(theme.Scroll))
	screen.SetContent(col, y, '▲', nil, style)
	screen.SetContent(col, y+h-1, '▼', nil, style)
	track := h - 2
	if track < 1 {
		return
	}
	// Согласуем единицы: offset+viewport не может превышать total (TextView
	// считает offset в строках после переноса, а total — в исходных).
	if offset+viewport > total {
		total = offset + viewport
	}
	// Нет прокрутки — дорожка целиком из ползунка.
	if total <= viewport {
		for i := 0; i < track; i++ {
			screen.SetContent(col, y+1+i, '█', nil, style)
		}
		return
	}
	size := track * viewport / total
	if size < 1 {
		size = 1
	}
	if size > track {
		size = track
	}
	maxOff := total - viewport
	pos := (track - size) * offset / maxOff
	if pos < 0 {
		pos = 0
	}
	if pos > track-size {
		pos = track - size
	}
	for i := 0; i < track; i++ {
		ch := '░'
		if i >= pos && i < pos+size {
			ch = '█'
		}
		screen.SetContent(col, y+1+i, ch, nil, style)
	}
}

// borlandBar рисует подсказки горячих клавиш в стиле статус-строки Turbo Pascal:
// серый фон, клавиша и описание чёрным, клавиша — жирная. Каждый пункт —
// кликабельный регион с идентификатором id (см. runStatusAction).
func borlandBar(items [][3]string) string {
	var b strings.Builder
	for _, it := range items {
		// Клавиша — акцентная жирная (как акселераторы в меню-баре), описание — текст бара.
		fmt.Fprintf(&b, ` ["%s"][%s::b]%s[%s::-] %s [""]`, it[0], theme.BarAccel, it[1], theme.BarFg, it[2])
	}
	return b.String()
}

func statusHints() string {
	return borlandBar([][3]string{
		{"help", "F1", "Справка"}, {"menu", "F10", "Меню"},
		{"tree", "^B", "Чаты"}, {"details", "Alt+I", "Инфо"},
		{"send", "Enter", "Отпр"}, {"quit", "Alt+X", "Выход"},
	})
}

func msgHints() string {
	return borlandBar([][3]string{
		{"tab", "Tab", "Элемент"}, {"copy", "c", "Копир"}, {"quote", "r", "Цитата"},
		{"del", "d", "Удал"}, {"mark", "Spc", "Выбор"}, {"open", "o", "Откр"},
		{"back", "Esc", "Чаты"}, {"menu", "F10", "Меню"},
	})
}

// treeHints — подсказки панели «Чаты». «К переписке» (Esc) показывается, только
// когда есть открытый чат, к которому можно вернуться.
func (u *ui) treeHints() string {
	items := [][3]string{
		{"sel", "Enter", "Открыть"}, {"nav", "↑↓", "Выбор"}, {"exp", "←→", "Свернуть"},
		{"fav", "^D", "В избранное"},
	}
	if u.favNode != nil {
		items = append(items, [3]string{"unfav", "d", "Убрать"})
	}
	if u.open != nil {
		items = append(items, [3]string{"back", "Esc", "К переписке"})
	}
	items = append(items,
		[3]string{"theme", "F8", "Тема"},
		[3]string{"menu", "F10", "Меню"})
	return borlandBar(items)
}

// inputHints — подсказки панели «Ввод».
func inputHints() string {
	return borlandBar([][3]string{
		{"send", "Enter", "Отпр"}, {"nl", "Alt+↵", "Перенос"},
		{"back", "Esc", "К сообщениям"}, {"menu", "F10", "Меню"},
	})
}

// detailsHints — подсказки панели «Информация».
func detailsHints() string {
	return borlandBar([][3]string{
		{"copy", "c", "Копир"}, {"nav", "↑↓", "Поле"}, {"back", "Esc", "Закрыть"}, {"menu", "F10", "Меню"},
	})
}

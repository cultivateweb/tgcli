// Package tui реализует интерактивный интерфейс tgcli на bubbletea.
//
// Раскладка: верхняя панель (имя/версия + открытый чат), снизу статус, между
// ними — левая панель чатов, центр (сообщения над полем ввода) и правая панель
// деталей. Левая и правая панели прячутся по хоткеям. Все размеры считаются
// точно под ширину/высоту терминала, чтобы рамки не переполняли экран.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cultivateweb/tgcli/internal/cache"
	"github.com/cultivateweb/tgcli/internal/telegram"
)

const (
	leftPanelW  = 30
	rightPanelW = 30
	headerH     = 4 // 2 строки контента + рамка
	statusH     = 1
	inputBoxH   = 5 // 3 строки ввода + рамка
)

// Run запускает TUI. c и updates могут быть nil (без кеша / без live).
func Run(ctx context.Context, sess *telegram.Session, c *cache.Cache, updates <-chan telegram.NewMessage, version string) error {
	p := tea.NewProgram(newModel(ctx, sess, c, version), tea.WithAltScreen(), tea.WithContext(ctx))
	if updates != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case nm, ok := <-updates:
					if !ok {
						return
					}
					p.Send(liveMsg{nm})
				}
			}
		}()
	}
	_, err := p.Run()
	return err
}

type focusArea int

const (
	focusList focusArea = iota
	focusInput
)

type model struct {
	ctx     context.Context
	sess    *telegram.Session
	cache   *cache.Cache
	version string

	dialogs        []telegram.Dialog
	dialogsFromNet bool
	sel, top       int

	open    *telegram.Dialog // открытый чат
	history []telegram.HistoryMessage
	loading bool

	input               textarea.Model
	focus               focusArea
	showLeft, showRight bool

	width, height int
	status        string
	flash         string // мигающее уведомление о новом сообщении
}

type dialogsMsg struct {
	d      []telegram.Dialog
	err    error
	cached bool
}
type historyMsg struct {
	key    string
	h      []telegram.HistoryMessage
	err    error
	cached bool
}
type sentMsg struct{ err error }
type liveMsg struct{ nm telegram.NewMessage }

// Палитра (truecolor) и стили.
var (
	accent     = lipgloss.Color("#7aa2f7")
	borderDim  = lipgloss.Color("#3b4261")
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7dcfff"))
	verStyle   = lipgloss.NewStyle().Faint(true)
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	selStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(accent)
	outStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a"))
	flashStyle = lipgloss.NewStyle().Bold(true).Blink(true).Foreground(lipgloss.Color("#f7768e"))

	kindColor = map[string]lipgloss.Color{
		"self":    lipgloss.Color("#7dcfff"), // голубой — Избранное
		"user":    lipgloss.Color("#9ece6a"), // зелёный — люди
		"bot":     lipgloss.Color("#e0af68"), // жёлтый — боты
		"group":   lipgloss.Color("#7aa2f7"), // синий — группы
		"channel": lipgloss.Color("#bb9af7"), // фиолетовый — каналы
	}
)

func newModel(ctx context.Context, sess *telegram.Session, c *cache.Cache, version string) model {
	ta := textarea.New()
	ta.Placeholder = "Сообщение…  (Enter — отправить, Alt+Enter — перенос строки)"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))

	saved := telegram.Dialog{Title: "Saved Messages", Kind: "user", Ref: telegram.PeerRef{Type: "self"}}
	saved.Peer = saved.Ref.InputPeer()

	return model{
		ctx:       ctx,
		sess:      sess,
		cache:     c,
		version:   version,
		input:     ta,
		focus:     focusList,
		showLeft:  true,
		showRight: false,
		open:      &saved,
		loading:   true,
		status:    "Загрузка…",
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.loadDialogsCache(), m.loadDialogsNet(),
		m.loadHistoryCache(*m.open), m.loadHistoryNet(*m.open),
	)
}

// ── Команды загрузки ───────────────────────────────────────────────────────

func (m model) loadDialogsCache() tea.Cmd {
	return func() tea.Msg {
		if m.cache == nil {
			return nil
		}
		d, err := m.cache.Dialogs()
		if err != nil || len(d) == 0 {
			return nil
		}
		return dialogsMsg{d: d, cached: true}
	}
}

func (m model) loadDialogsNet() tea.Cmd {
	return func() tea.Msg {
		d, err := m.sess.Dialogs(m.ctx, 100, false)
		if err == nil && m.cache != nil {
			_ = m.cache.SaveDialogs(d)
		}
		return dialogsMsg{d: d, err: err}
	}
}

func (m model) loadHistoryCache(d telegram.Dialog) tea.Cmd {
	key := d.Ref.Key()
	return func() tea.Msg {
		if m.cache == nil {
			return nil
		}
		h, err := m.cache.History(key)
		if err != nil || len(h) == 0 {
			return nil
		}
		return historyMsg{key: key, h: h, cached: true}
	}
}

func (m model) loadHistoryNet(d telegram.Dialog) tea.Cmd {
	key := d.Ref.Key()
	return func() tea.Msg {
		h, err := m.sess.HistoryByPeer(m.ctx, d.Peer, 50)
		if err == nil && m.cache != nil {
			_ = m.cache.SaveHistory(key, h)
		}
		return historyMsg{key: key, h: h, err: err}
	}
}

func (m model) send(d telegram.Dialog, text string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.sess.SendToPeer(m.ctx, d.Peer, text)
		return sentMsg{err}
	}
}

// ── Update ─────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeInput()
		return m, nil

	case dialogsMsg:
		if msg.cached && m.dialogsFromNet {
			return m, nil
		}
		if msg.err != nil {
			if len(m.dialogs) == 0 {
				m.status = "Ошибка диалогов: " + msg.err.Error()
			}
			return m, nil
		}
		m.dialogs = msg.d
		if !msg.cached {
			m.dialogsFromNet = true
		}
		m.status = ""
		return m, nil

	case historyMsg:
		if m.open == nil || msg.key != m.open.Ref.Key() {
			return m, nil // история не от текущего чата
		}
		if msg.cached && !m.loading {
			return m, nil
		}
		if msg.err != nil {
			m.loading = false
			m.status = "Ошибка истории: " + msg.err.Error()
			return m, nil
		}
		m.history = msg.h
		if !msg.cached {
			m.loading = false
			m.status = ""
		}
		return m, nil

	case sentMsg:
		if msg.err != nil {
			m.status = "Ошибка отправки: " + msg.err.Error()
			return m, nil
		}
		m.input.Reset()
		if m.open != nil {
			return m, m.loadHistoryNet(*m.open)
		}
		return m, nil

	case liveMsg:
		return m.handleLive(msg.nm)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.focus == focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) resizeInput() {
	cw := m.centerWidth() - 2 // минус рамка
	if cw < 4 {
		cw = 4
	}
	m.input.SetWidth(cw)
	m.input.SetHeight(inputBoxH - 2)
}

func (m model) centerWidth() int {
	w := m.width
	if m.showLeft {
		w -= leftPanelW
	}
	if m.showRight {
		w -= rightPanelW
	}
	if w < 10 {
		w = 10
	}
	return w
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+b":
		m.showLeft = !m.showLeft
		m.resizeInput()
		return m, nil
	case "ctrl+e":
		m.showRight = !m.showRight
		m.resizeInput()
		return m, nil
	case "tab":
		if m.focus == focusList {
			m.focus = focusInput
			m.input.Focus()
			return m, textarea.Blink
		}
		m.focus = focusList
		m.input.Blur()
		return m, nil
	}

	if m.focus == focusInput {
		return m.handleInputKey(msg)
	}
	return m.handleListKey(msg)
}

func (m model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.sel > 0 {
			m.sel--
		}
	case "down", "j":
		if m.sel < len(m.dialogs)-1 {
			m.sel++
		}
	case "enter":
		if cur, ok := m.current(); ok {
			return m.openChat(cur)
		}
	}
	return m, nil
}

func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusList
		m.input.Blur()
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" || m.open == nil {
			return m, nil
		}
		m.status = "Отправка…"
		return m, m.send(*m.open, text)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) openChat(d telegram.Dialog) (tea.Model, tea.Cmd) {
	dd := d
	m.open = &dd
	m.history = nil
	m.loading = true
	m.flash = ""
	// сбросить непрочитанные у открытого чата
	for i := range m.dialogs {
		if m.dialogs[i].Ref.Key() == dd.Ref.Key() {
			m.dialogs[i].Unread = 0
		}
	}
	return m, tea.Batch(m.loadHistoryCache(dd), m.loadHistoryNet(dd))
}

func (m model) handleLive(nm telegram.NewMessage) (tea.Model, tea.Cmd) {
	openKey := ""
	if m.open != nil {
		openKey = m.open.Ref.Key()
	}
	if nm.PeerKey == openKey {
		m.history = append(m.history, nm.Message)
		if m.cache != nil {
			_ = m.cache.SaveHistory(openKey, m.history)
		}
	}
	for i := range m.dialogs {
		if m.dialogs[i].Ref.Key() == nm.PeerKey {
			m.dialogs[i].Date = nm.Message.Date
			if nm.PeerKey != openKey && !nm.Message.Out {
				m.dialogs[i].Unread++
				m.flash = "● новое сообщение: " + truncate(m.dialogs[i].Title, 30)
			}
			break
		}
	}
	return m, nil
}

func (m model) current() (telegram.Dialog, bool) {
	if m.sel >= 0 && m.sel < len(m.dialogs) {
		return m.dialogs[m.sel], true
	}
	return telegram.Dialog{}, false
}

// ── View ───────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width < 40 || m.height < 14 {
		return "Окно слишком маленькое — увеличьте терминал."
	}
	midH := m.height - headerH - statusH

	header := m.renderHeader()

	var cols []string
	if m.showLeft {
		cols = append(cols, m.renderChats(leftPanelW-2, midH-2))
	}
	cols = append(cols, m.renderCenter(m.centerWidth(), midH))
	if m.showRight {
		cols = append(cols, m.renderDetails(rightPanelW-2, midH-2))
	}
	mid := lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	return lipgloss.JoinVertical(lipgloss.Left, header, mid, m.renderStatus())
}

func (m model) renderHeader() string {
	name := "—"
	if m.open != nil {
		name = m.open.Title
	}
	line1 := titleStyle.Render("tgcli") + " " + verStyle.Render(m.version)
	line2 := dimStyle.Render("Чат: ") + truncate(name, m.width-8)
	content := clip([]string{line1, line2}, m.width-2, 2, false)
	return roundBox(content, m.width-2, 2, borderDim)
}

func (m model) renderStatus() string {
	hints := "Tab — фокус • Ctrl+B/Ctrl+E — панели • Enter — отпр./открыть • q — выход"
	s := hints
	if m.flash != "" {
		s = flashStyle.Render(m.flash) + "  " + dimStyle.Render(hints)
	} else if m.status != "" {
		s = dimStyle.Render(m.status) + "  " + dimStyle.Render(hints)
	}
	return truncate(s, m.width)
}

func (m model) renderChats(w, h int) string {
	top := scrollTop(m.sel, m.top, h)
	var lines []string
	for i := top; i < len(m.dialogs) && i < top+h; i++ {
		d := m.dialogs[i]
		// Собираем plain-текст и обрезаем до окраски — иначе truncate режет ANSI.
		plain := "● " + d.Title
		if d.Unread > 0 {
			plain += fmt.Sprintf(" (%d)", d.Unread)
		}
		plain = truncate(plain, w)
		if i == m.sel {
			lines = append(lines, selStyle.Render(padTo(plain, w)))
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorOf(d)).Render(plain))
		}
	}
	border := borderDim
	if m.focus == focusList {
		border = accent
	}
	return roundBox(strings.Join(lines, "\n"), w, h, border)
}

func (m model) renderCenter(outerW, midH int) string {
	w := outerW - 2 // контент внутри рамки
	msgBoxH := midH - inputBoxH
	msgs := m.renderMessages(w, msgBoxH-2)
	msgBox := roundBox(msgs, w, msgBoxH-2, borderDim)

	inputBorder := borderDim
	if m.focus == focusInput {
		inputBorder = accent
	}
	inBox := roundBox(m.input.View(), w, inputBoxH-2, inputBorder)
	return lipgloss.JoinVertical(lipgloss.Left, msgBox, inBox)
}

func (m model) renderMessages(w, h int) string {
	if m.loading {
		return dimStyle.Render("Загрузка истории…")
	}
	var lines []string
	for _, msg := range m.history {
		text := strings.ReplaceAll(msg.Text, "\n", " ")
		if text == "" {
			text = "[вложение]"
		}
		// Обрезаем plain-текст до окраски — иначе truncate режет ANSI.
		if msg.Out {
			lines = append(lines, outStyle.Render(truncate("→ "+text, w)))
		} else {
			lines = append(lines, truncate(truncate(msg.Author, 16)+": "+text, w))
		}
	}
	if len(lines) > h && h > 0 {
		lines = lines[len(lines)-h:]
	}
	return strings.Join(lines, "\n")
}

func (m model) renderDetails(w, h int) string {
	var lines []string
	if m.open != nil {
		lines = append(lines,
			titleStyle.Render("Детали"),
			"",
			dimStyle.Render("Имя:"), truncate(m.open.Title, w),
			dimStyle.Render("Тип:"), m.open.Kind,
			dimStyle.Render("ID:"), m.open.Ref.Key(),
		)
	}
	return roundBox(clip(lines, w, h, false), w, h, borderDim)
}

// ── Хелперы раскладки ──────────────────────────────────────────────────────

// roundBox оборачивает контент скруглённой рамкой с фиксированными размерами.
func roundBox(content string, w, h int, border lipgloss.Color) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Width(w).Height(h).
		Render(content)
}

// clip обрезает каждую строку до ширины w и список до h строк (снизу или сверху).
func clip(lines []string, w, h int, fromBottom bool) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, truncate(l, w))
	}
	if len(out) > h && h > 0 {
		if fromBottom {
			out = out[len(out)-h:]
		} else {
			out = out[:h]
		}
	}
	return strings.Join(out, "\n")
}

// scrollTop вычисляет первую видимую строку списка, удерживая выбранную в окне.
func scrollTop(sel, top, rows int) int {
	if sel < top {
		top = sel
	}
	if sel >= top+rows {
		top = sel - rows + 1
	}
	if top < 0 {
		top = 0
	}
	return top
}

func colorOf(d telegram.Dialog) lipgloss.Color {
	if d.Ref.Type == "self" {
		return kindColor["self"]
	}
	if c, ok := kindColor[d.Kind]; ok {
		return c
	}
	return lipgloss.Color("#c0caf5")
}

// padTo дополняет строку пробелами до n колонок (для подсветки во всю ширину).
func padTo(s string, n int) string {
	if d := n - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// truncate обрезает строку до n колонок по визуальной ширине (с учётом эмодзи).
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

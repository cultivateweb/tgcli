// Package tui реализует лёгкий интерактивный интерфейс tgcli на bubbletea.
//
// Два экрана: список чатов и открытый чат. История грузится только при входе
// в чат (Enter), а не при перемещении по списку — так интерфейс остаётся
// лёгким. Рисуем плоскими строками (без рамок и панелей), чтобы избежать
// проблем с раскладкой: каждая строка обрезается по ширине терминала.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cultivateweb/tgcli/internal/cache"
	"github.com/cultivateweb/tgcli/internal/telegram"
)

// Run запускает TUI поверх открытой сессии (соединение держится всё время).
// c может быть nil — тогда работаем без кеша. updates может быть nil — тогда
// без live-обновлений; иначе входящие сообщения прилетают в интерфейс сами.
func Run(ctx context.Context, sess *telegram.Session, c *cache.Cache, updates <-chan telegram.NewMessage) error {
	p := tea.NewProgram(newModel(ctx, sess, c), tea.WithAltScreen(), tea.WithContext(ctx))
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

type screen int

const (
	listScreen screen = iota
	chatScreen
)

type model struct {
	ctx   context.Context
	sess  *telegram.Session
	cache *cache.Cache

	dialogs        []telegram.Dialog
	dialogsFromNet bool
	sel            int
	top            int // индекс первого видимого чата в списке (прокрутка)

	screen  screen
	history []telegram.HistoryMessage
	loading bool
	openTo  telegram.Dialog
	input   textinput.Model

	width, height int
	status        string
}

// Сообщения от асинхронных команд. cached = данные пришли из локального кеша.
type dialogsMsg struct {
	d      []telegram.Dialog
	err    error
	cached bool
}
type historyMsg struct {
	h      []telegram.HistoryMessage
	err    error
	cached bool
}
type sentMsg struct{ err error }

// liveMsg — входящее сообщение из потока обновлений.
type liveMsg struct{ nm telegram.NewMessage }

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	selStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	outStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func newModel(ctx context.Context, sess *telegram.Session, c *cache.Cache) model {
	ti := textinput.New()
	ti.Placeholder = "Сообщение…"
	ti.CharLimit = 4096
	return model{
		ctx:    ctx,
		sess:   sess,
		cache:  c,
		input:  ti,
		screen: listScreen,
		status: "Загрузка диалогов…",
	}
}

func (m model) Init() tea.Cmd {
	// Сначала кеш (мгновенно), затем сеть (обновит).
	return tea.Batch(m.loadDialogsCache(), m.loadDialogsNet())
}

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
	return func() tea.Msg {
		if m.cache == nil {
			return nil
		}
		h, err := m.cache.History(d.Ref.Key())
		if err != nil || len(h) == 0 {
			return nil
		}
		return historyMsg{h: h, cached: true}
	}
}

func (m model) loadHistoryNet(d telegram.Dialog) tea.Cmd {
	return func() tea.Msg {
		h, err := m.sess.HistoryByPeer(m.ctx, d.Peer, 40)
		if err == nil && m.cache != nil {
			_ = m.cache.SaveHistory(d.Ref.Key(), h)
		}
		return historyMsg{h: h, err: err}
	}
}

func (m model) send(d telegram.Dialog, text string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.sess.SendToPeer(m.ctx, d.Peer, text)
		return sentMsg{err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = m.width - 4
		if m.input.Width < 4 {
			m.input.Width = 4
		}
		return m, nil

	case dialogsMsg:
		if msg.cached && m.dialogsFromNet {
			return m, nil // сеть уже обновила — не затираем кешем
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
		m.status = listStatus(len(m.dialogs), msg.cached)
		return m, nil

	case historyMsg:
		if msg.cached && !m.loading {
			return m, nil // сеть уже обновила — не затираем кешем
		}
		if msg.err != nil {
			m.loading = false
			m.status = "Ошибка истории: " + msg.err.Error()
			return m, nil
		}
		m.history = msg.h
		if !msg.cached {
			m.loading = false
		}
		m.status = chatStatus()
		return m, nil

	case sentMsg:
		if msg.err != nil {
			m.status = "Ошибка отправки: " + msg.err.Error()
			return m, nil
		}
		m.input.SetValue("")
		return m, m.loadHistoryNet(m.openTo)

	case liveMsg:
		return m.handleLive(msg.nm)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleLive обрабатывает входящее сообщение: дописывает в открытый чат и
// обновляет счётчик непрочитанных в списке.
func (m model) handleLive(nm telegram.NewMessage) (tea.Model, tea.Cmd) {
	openKey := ""
	if m.screen == chatScreen {
		openKey = m.openTo.Ref.Key()
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
			m.dialogs[i].Preview = strings.ReplaceAll(nm.Message.Text, "\n", " ")
			if nm.PeerKey != openKey && !nm.Message.Out {
				m.dialogs[i].Unread++
			}
			break
		}
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.screen == chatScreen {
		return m.handleChatKey(msg)
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
		cur, ok := m.current()
		if !ok {
			return m, nil
		}
		m.screen = chatScreen
		m.openTo = cur
		m.history = nil
		m.loading = true
		m.status = "Загрузка истории…"
		m.input.Focus()
		return m, tea.Batch(m.loadHistoryCache(cur), m.loadHistoryNet(cur), textinput.Blink)
	}
	return m, nil
}

func (m model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = listScreen
		m.input.Blur()
		m.input.SetValue("")
		m.status = listStatus(len(m.dialogs), false)
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.status = "Отправка…"
		return m, m.send(m.openTo, text)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) current() (telegram.Dialog, bool) {
	if m.sel >= 0 && m.sel < len(m.dialogs) {
		return m.dialogs[m.sel], true
	}
	return telegram.Dialog{}, false
}

func listStatus(n int, cached bool) string {
	s := fmt.Sprintf("Диалогов: %d  •  ↑↓ — выбор, Enter — открыть, q — выход", n)
	if cached {
		s += "  (кеш)"
	}
	return s
}

func chatStatus() string {
	return "Enter — отправить, Esc — назад к списку, Ctrl+C — выход"
}

func (m model) View() string {
	if m.width < 10 || m.height < 4 {
		return "Окно слишком маленькое."
	}
	if m.screen == chatScreen {
		return m.viewChat()
	}
	return m.viewList()
}

func (m *model) ensureVisible(rows int) {
	// Держим выбранный чат в видимом окне [top, top+rows).
	if m.sel < m.top {
		m.top = m.sel
	}
	if m.sel >= m.top+rows {
		m.top = m.sel - rows + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

func (m model) viewList() string {
	rows := m.height - 2 // заголовок + строка статуса
	if rows < 1 {
		rows = 1
	}
	m.ensureVisible(rows)

	var b strings.Builder
	b.WriteString(titleStyle.Render(truncate("tgcli — чаты", m.width)))
	b.WriteByte('\n')

	for i := m.top; i < len(m.dialogs) && i < m.top+rows; i++ {
		d := m.dialogs[i]
		cursor := "  "
		if i == m.sel {
			cursor = "▌ "
		}
		mark := ""
		if d.Unread > 0 {
			mark = fmt.Sprintf("(%d) ", d.Unread)
		}
		line := truncate(cursor+mark+d.Title, m.width)
		if i == m.sel {
			line = selStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	// Дополняем до нижней строки, чтобы статус был внизу.
	shown := len(m.dialogs) - m.top
	if shown > rows {
		shown = rows
	}
	for i := shown; i < rows; i++ {
		b.WriteByte('\n')
	}
	b.WriteString(dimStyle.Render(truncate(m.status, m.width)))
	return b.String()
}

func (m model) viewChat() string {
	bodyH := m.height - 3 // заголовок + поле ввода + статус
	if bodyH < 1 {
		bodyH = 1
	}

	var lines []string
	if m.loading {
		lines = append(lines, dimStyle.Render("Загрузка…"))
	} else {
		for _, msg := range m.history {
			text := strings.ReplaceAll(msg.Text, "\n", " ")
			if text == "" {
				text = "[вложение]"
			}
			if msg.Out {
				lines = append(lines, outStyle.Render(truncate("→ "+text, m.width)))
			} else {
				lines = append(lines, truncate(truncate(msg.Author, 18)+": "+text, m.width))
			}
		}
	}
	if len(lines) > bodyH {
		lines = lines[len(lines)-bodyH:]
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(truncate("← "+m.openTo.Title, m.width)))
	b.WriteByte('\n')
	b.WriteString(strings.Join(lines, "\n"))
	for i := len(lines); i < bodyH; i++ {
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(truncate(m.input.View(), m.width))
	b.WriteByte('\n')
	b.WriteString(dimStyle.Render(truncate(m.status, m.width)))
	return b.String()
}

// truncate обрезает строку до n колонок по визуальной ширине (учитывая
// двухклеточные эмодзи), добавляя многоточие.
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

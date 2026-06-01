// Package tui реализует интерактивный интерфейс tgcli на bubbletea:
// слева список диалогов, справа переписка, снизу поле ввода.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

// Run запускает TUI поверх открытой сессии (соединение держится всё время).
func Run(ctx context.Context, sess *telegram.Session) error {
	p := tea.NewProgram(newModel(ctx, sess), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

type focusArea int

const (
	focusList focusArea = iota
	focusInput
)

type model struct {
	ctx  context.Context
	sess *telegram.Session

	dialogs []telegram.Dialog
	sel     int
	history []telegram.HistoryMessage

	input textinput.Model
	focus focusArea

	width, height int
	status        string
}

// Сообщения от асинхронных команд.
type dialogsMsg struct {
	d   []telegram.Dialog
	err error
}
type historyMsg struct {
	h   []telegram.HistoryMessage
	err error
}
type sentMsg struct{ err error }

var (
	listBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	chatBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	inputBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))
	outStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
)

func newModel(ctx context.Context, sess *telegram.Session) model {
	ti := textinput.New()
	ti.Placeholder = "Сообщение…  (Enter — отправить, Esc — к списку)"
	ti.CharLimit = 4096
	return model{
		ctx:    ctx,
		sess:   sess,
		input:  ti,
		focus:  focusList,
		status: "Загрузка диалогов…",
	}
}

func (m model) Init() tea.Cmd {
	return m.loadDialogs()
}

func (m model) loadDialogs() tea.Cmd {
	return func() tea.Msg {
		d, err := m.sess.Dialogs(m.ctx, 50, false)
		return dialogsMsg{d, err}
	}
}

func (m model) loadHistory(d telegram.Dialog) tea.Cmd {
	return func() tea.Msg {
		h, err := m.sess.HistoryByPeer(m.ctx, d.Peer, 40)
		return historyMsg{h, err}
	}
}

func (m model) send(d telegram.Dialog, text string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.sess.SendToPeer(m.ctx, d.Peer, text)
		return sentMsg{err}
	}
}

func (m model) current() (telegram.Dialog, bool) {
	if m.sel >= 0 && m.sel < len(m.dialogs) {
		return m.dialogs[m.sel], true
	}
	return telegram.Dialog{}, false
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case dialogsMsg:
		if msg.err != nil {
			m.status = "Ошибка диалогов: " + msg.err.Error()
			return m, nil
		}
		m.dialogs = msg.d
		m.status = fmt.Sprintf("Диалогов: %d  •  ↑↓ выбор, Enter — ввод, q — выход", len(m.dialogs))
		if len(m.dialogs) > 0 {
			m.sel = 0
			return m, m.loadHistory(m.dialogs[0])
		}
		return m, nil

	case historyMsg:
		if msg.err != nil {
			m.status = "Ошибка истории: " + msg.err.Error()
			return m, nil
		}
		m.history = msg.h
		return m, nil

	case sentMsg:
		if msg.err != nil {
			m.status = "Ошибка отправки: " + msg.err.Error()
			return m, nil
		}
		m.input.SetValue("")
		m.status = "Отправлено."
		if cur, ok := m.current(); ok {
			return m, m.loadHistory(cur)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	if m.focus == focusInput {
		switch msg.String() {
		case "esc":
			m.focus = focusList
			m.input.Blur()
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			cur, ok := m.current()
			if !ok {
				return m, nil
			}
			m.status = "Отправка…"
			return m, m.send(cur, text)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	// Фокус на списке.
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.sel > 0 {
			m.sel--
			if cur, ok := m.current(); ok {
				return m, m.loadHistory(cur)
			}
		}
		return m, nil
	case "down", "j":
		if m.sel < len(m.dialogs)-1 {
			m.sel++
			if cur, ok := m.current(); ok {
				return m, m.loadHistory(cur)
			}
		}
		return m, nil
	case "enter", "tab", "i":
		m.focus = focusInput
		m.input.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Загрузка…"
	}

	sidebarOuter := 36
	if sidebarOuter > m.width/2 {
		sidebarOuter = m.width / 2
	}
	mainOuter := m.width - sidebarOuter
	bodyH := m.height - 1 // строка статуса снизу

	inputOuter := 3 // рамка + строка
	chatH := bodyH - inputOuter

	sidebar := m.renderList(sidebarOuter-2, bodyH-2)
	chat := m.renderChat(mainOuter-2, chatH-2)
	input := m.renderInput(mainOuter - 2)
	right := lipgloss.JoinVertical(lipgloss.Left, chat, input)
	row := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, right)

	return lipgloss.JoinVertical(lipgloss.Left, row, dimStyle.Render(truncate(m.status, m.width)))
}

func (m model) renderList(w, h int) string {
	var lines []string
	start := 0
	if len(m.dialogs) > h && m.sel >= h {
		start = m.sel - h + 1
	}
	for i := start; i < len(m.dialogs) && i < start+h; i++ {
		d := m.dialogs[i]
		mark := "  "
		if d.Unread > 0 {
			mark = "● "
		}
		line := mark + truncate(d.Title, w-2)
		if i == m.sel {
			line = selStyle.Render(padRight(line, w))
		}
		lines = append(lines, line)
	}
	style := listBox.Width(w).Height(h)
	if m.focus == focusList {
		style = style.BorderForeground(lipgloss.Color("6"))
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m model) renderChat(w, h int) string {
	var lines []string
	for _, msg := range m.history {
		text := strings.ReplaceAll(msg.Text, "\n", " ")
		if text == "" {
			text = "[вложение]"
		}
		var head string
		if msg.Out {
			head = outStyle.Render("→ ")
		} else {
			head = nameStyle.Render(truncate(msg.Author, 18) + ": ")
		}
		lines = append(lines, truncate(head+text, w))
	}
	if len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	return chatBox.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

func (m model) renderInput(w int) string {
	style := inputBox.Width(w)
	if m.focus == focusInput {
		style = style.BorderForeground(lipgloss.Color("6"))
	}
	return style.Render(m.input.View())
}

// truncate обрезает строку до n рун, добавляя многоточие.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// padRight дополняет строку пробелами до n рун (для подсветки во всю ширину).
func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(r))
}

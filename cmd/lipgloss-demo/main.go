// Команда lipgloss-demo печатает витрину возможностей lipgloss, чтобы выбрать
// визуальный язык для TUI. Запуск: go run ./cmd/lipgloss-demo
package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func main() {
	section("1. Базовые цвета ANSI (0–15)")
	swatchRow(0, 16)

	section("2. Палитра 256 цветов (градиент)")
	for _, base := range [][2]int{{16, 51}, {52, 87}, {88, 123}, {124, 159}, {160, 195}, {196, 231}} {
		swatchRow(base[0], base[1]+1)
	}

	section("3. Truecolor — плавный градиент (24-битный цвет)")
	gradient()

	section("4. Цвет фона")
	for _, c := range []string{"1", "2", "3", "4", "5", "6"} {
		fmt.Print(lipgloss.NewStyle().Background(lipgloss.Color(c)).Foreground(lipgloss.Color("0")).Render("  фон " + c + "  "))
		fmt.Print(" ")
	}
	fmt.Println()

	section("5. Начертания текста")
	styles := []struct {
		name string
		st   lipgloss.Style
	}{
		{"bold (жирный)", lipgloss.NewStyle().Bold(true)},
		{"faint (тусклый)", lipgloss.NewStyle().Faint(true)},
		{"italic (курсив)", lipgloss.NewStyle().Italic(true)},
		{"underline (подчёркнутый)", lipgloss.NewStyle().Underline(true)},
		{"strikethrough (зачёркнутый)", lipgloss.NewStyle().Strikethrough(true)},
		{"reverse (инверсия)", lipgloss.NewStyle().Reverse(true)},
		{"blink (мигание)", lipgloss.NewStyle().Blink(true)},
	}
	for _, s := range styles {
		fmt.Println("  " + s.st.Render(s.name))
	}

	section("6. Рамки")
	borders := []struct {
		name string
		b    lipgloss.Border
	}{
		{"rounded", lipgloss.RoundedBorder()},
		{"normal", lipgloss.NormalBorder()},
		{"thick", lipgloss.ThickBorder()},
		{"double", lipgloss.DoubleBorder()},
		{"ascii", lipgloss.ASCIIBorder()},
		{"block", lipgloss.BlockBorder()},
		{"hidden", lipgloss.HiddenBorder()},
	}
	var boxes []string
	for _, b := range borders {
		box := lipgloss.NewStyle().Border(b.b).Padding(0, 1).Render(b.name)
		boxes = append(boxes, box)
	}
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Top, joinWithGap(boxes, "  ")...))

	section("7. Раскладка: выравнивание в фиксированной ширине (24)")
	for _, a := range []struct {
		name string
		pos  lipgloss.Position
	}{{"влево", lipgloss.Left}, {"по центру", lipgloss.Center}, {"вправо", lipgloss.Right}} {
		box := lipgloss.NewStyle().Width(24).Align(a.pos).
			Border(lipgloss.RoundedBorder()).Render(a.name)
		fmt.Println(box)
	}

	section("8. Блоки рядом (JoinHorizontal) + отступы")
	left := lipgloss.NewStyle().Width(18).Height(3).Padding(1, 2).
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("6")).Render("панель A")
	right := lipgloss.NewStyle().Width(28).Height(3).Padding(1, 2).
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("5")).Render("панель B")
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right))

	section("9. Готовые элементы для TUI")

	// Заголовок.
	fmt.Println(lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("6")).
		Foreground(lipgloss.Color("0")).Padding(0, 1).Render(" tgcli — Saved Messages "))
	fmt.Println()

	// Список чатов с выделенной строкой и бейджами.
	rowSel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))
	badge := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("1")).Padding(0, 1)
	fmt.Println("  " + rowSel.Render(" ▌ Алиса Петрова            ") + " " + badge.Render("3"))
	fmt.Println("    Рабочий чат")
	fmt.Println("    Новости " + lipgloss.NewStyle().Faint(true).Render("(канал)"))
	fmt.Println()

	// Вкладки.
	fmt.Println("  " + tabs([]string{"Избранное", "Люди", "Боты", "Группы", "Каналы"}, 1))

	section("10. Предложение цветов по типам чата")
	kinds := []struct {
		name  string
		color string
	}{
		{"Избранное", "6"}, // голубой
		{"Люди", "2"},      // зелёный
		{"Боты", "3"},      // жёлтый
		{"Группы", "4"},    // синий
		{"Каналы", "5"},    // фиолетовый
	}
	for _, k := range kinds {
		dot := lipgloss.NewStyle().Foreground(lipgloss.Color(k.color)).Render("●")
		label := lipgloss.NewStyle().Foreground(lipgloss.Color(k.color)).Render(k.name)
		fmt.Printf("  %s %s\n", dot, label)
	}

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Faint(true).Render(
		"Скажи: какую палитру (ANSI/256/truecolor), начертания, тип рамки и цвета по типам берём в TUI."))
}

func section(title string) {
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(title))
	fmt.Println(strings.Repeat("─", lipgloss.Width(title)))
}

// swatchRow печатает цветные квадраты с кодами [from, to).
func swatchRow(from, to int) {
	var b strings.Builder
	for c := from; c < to; c++ {
		code := fmt.Sprintf("%d", c)
		sw := lipgloss.NewStyle().Background(lipgloss.Color(code)).Render(fmt.Sprintf(" %3s ", code))
		b.WriteString(sw)
	}
	fmt.Println(b.String())
}

// gradient печатает плавный truecolor-градиент.
func gradient() {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		r := 255 - i*4
		g := i * 4
		bl := 128
		hex := fmt.Sprintf("#%02x%02x%02x", clamp(r), clamp(g), clamp(bl))
		b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render(" "))
	}
	fmt.Println(b.String())
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// tabs рисует строку вкладок, выделяя активную (по индексу).
func tabs(names []string, active int) string {
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("6")).Padding(0, 1)
	idleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Padding(0, 1)
	var parts []string
	for i, n := range names {
		if i == active {
			parts = append(parts, activeStyle.Render(n))
		} else {
			parts = append(parts, idleStyle.Render(n))
		}
	}
	return strings.Join(parts, " ")
}

// joinWithGap вставляет разделитель между блоками для JoinHorizontal.
func joinWithGap(items []string, gap string) []string {
	if len(items) == 0 {
		return items
	}
	out := []string{items[0]}
	for _, it := range items[1:] {
		out = append(out, gap, it)
	}
	return out
}

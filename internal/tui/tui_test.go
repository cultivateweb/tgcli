package tui

import (
	"testing"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

func TestGroupKey(t *testing.T) {
	cases := []struct {
		d    telegram.Dialog
		want string
	}{
		{telegram.Dialog{Kind: "user", Ref: telegram.PeerRef{Type: "self"}}, "self"},
		{telegram.Dialog{Kind: "user", Ref: telegram.PeerRef{Type: "user"}}, "user"},
		{telegram.Dialog{Kind: "bot", Ref: telegram.PeerRef{Type: "user"}}, "bot"},
		{telegram.Dialog{Kind: "group", Ref: telegram.PeerRef{Type: "chat"}}, "group"},
		{telegram.Dialog{Kind: "group", Mine: true, Ref: telegram.PeerRef{Type: "chat"}}, "mygroup"},
		{telegram.Dialog{Kind: "supergroup", Ref: telegram.PeerRef{Type: "channel"}}, "group"},
		{telegram.Dialog{Kind: "supergroup", Mine: true, Ref: telegram.PeerRef{Type: "channel"}}, "mygroup"},
		{telegram.Dialog{Kind: "channel", Ref: telegram.PeerRef{Type: "channel"}}, "channel"},
		{telegram.Dialog{Kind: "channel", Mine: true, Ref: telegram.PeerRef{Type: "channel"}}, "mychannel"},
	}
	for _, c := range cases {
		if got := groupKey(c.d); got != c.want {
			t.Errorf("groupKey(%+v) = %q, ожидалось %q", c.d, got, c.want)
		}
	}
}

// TestTreeWidthEmoji проверяет, что разметка узла дерева занимает ровно width
// клеток даже при эмодзи-флагах и новых эмодзи (иначе колонки «съезжают»).
func TestTreeWidthEmoji(t *testing.T) {
	const width = 50
	titles := []string{
		"Lviv Python Community 🪖🇺🇦",
		"🇺🇦🇺🇦🇺🇦 флаги",
		"обычный канал",
	}
	for _, ti := range titles {
		if w := cellsWidth(treeRow(ti, "", "(3)", width)); w != width {
			t.Errorf("treeRow(%q) ширина = %d, ожидалось %d", ti, w, width)
		}
		if w := cellsWidth(treeLine(ti, "(3)", width)); w != width {
			t.Errorf("treeLine(%q) ширина = %d, ожидалось %d", ti, w, width)
		}
	}
}

func TestStableLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"🔔 Активные   (3)", "🔔 Активные"},
		{"★ Избранное (12)", "★ Избранное"},
		{"👤 Люди      (40)", "👤 Люди"},
		{"💾 Saved Messages", "💾 Saved Messages"},
		{"  без счётчика  ", "без счётчика"},
	}
	for _, c := range cases {
		if got := stableLabel(c.in); got != c.want {
			t.Errorf("stableLabel(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

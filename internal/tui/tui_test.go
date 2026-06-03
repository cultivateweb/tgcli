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

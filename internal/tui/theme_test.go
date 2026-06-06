package tui

import "testing"

func TestResolveThemeName(t *testing.T) {
	ok := map[string]string{
		"tokyo":            "Tokyo Night",
		"Tokyo Night":      "Tokyo Night",
		"tokyonight":       "Tokyo Night",
		"TOKYO":            "Tokyo Night",
		"mocha":            "Catppuccin Mocha",
		"catppuccin":       "Catppuccin Mocha",
		"Catppuccin Mocha": "Catppuccin Mocha",
		"gruvbox":          "Gruvbox Dark",
		"gruvbox-dark":     "Gruvbox Dark",
		"nord":             "Nord",
		" Nord ":           "Nord",
	}
	for in, want := range ok {
		got, found := ResolveThemeName(in)
		if !found || got != want {
			t.Errorf("ResolveThemeName(%q) = (%q, %v), want (%q, true)", in, got, found, want)
		}
	}
	for _, bad := range []string{"", "solarized", "dracula", "tok"} {
		if _, found := ResolveThemeName(bad); found {
			t.Errorf("ResolveThemeName(%q): ожидалась ошибка, тема найдена", bad)
		}
	}
}

// TestThemesComplete страхует от пустых цветовых полей в любой из тем: пустая
// hex-строка в tcell.GetColor даёт чёрный, что выглядит как «дыра» в палитре.
func TestThemesComplete(t *testing.T) {
	for _, th := range themes {
		if th.Name == "" {
			t.Error("тема без имени")
		}
		for name, v := range map[string]string{
			"BgWindow": th.BgWindow, "BgPanel": th.BgPanel, "Text": th.Text,
			"TextBright": th.TextBright, "TextDim": th.TextDim, "Inverse": th.Inverse,
			"BorderActive": th.BorderActive, "TitleActive": th.TitleActive, "Inactive": th.Inactive,
			"BarBg": th.BarBg, "BarFg": th.BarFg, "BarAccel": th.BarAccel,
			"MenuText": th.MenuText, "MenuSelBg": th.MenuSelBg, "MenuSelFg": th.MenuSelFg,
			"MenuAccel": th.MenuAccel, "Shadow": th.Shadow, "ShadowFg": th.ShadowFg,
			"Scroll": th.Scroll, "Contrast": th.Contrast, "MoreContrast": th.MoreContrast,
			"MsgBg": th.MsgBg, "MsgBgAlt": th.MsgBgAlt, "MsgSel": th.MsgSel,
			"MsgOut": th.MsgOut, "MsgAuthor": th.MsgAuthor, "MsgCode": th.MsgCode, "MsgLink": th.MsgLink,
			"Success": th.Success, "ErrorC": th.ErrorC, "Warn": th.Warn, "Info": th.Info, "Accent": th.Accent,
			"Self": th.Self, "User": th.User, "Bot": th.Bot, "Group": th.Group,
			"Supergroup": th.Supergroup, "Channel": th.Channel, "Unread": th.Unread,
		} {
			if v == "" {
				t.Errorf("%s: пустое поле %s", th.Name, name)
			}
		}
	}
}

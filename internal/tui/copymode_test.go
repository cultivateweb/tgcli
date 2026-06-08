package tui

import "testing"

// segText восстанавливает видимые строки из сегментов для удобного сравнения.
func segText(text []rune, segs []textSeg) []string {
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = string(text[s.start:s.end])
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWrapText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		w    int
		want []string
	}{
		{"пустой", "", 5, []string{""}},
		{"короткий", "abc", 5, []string{"abc"}},
		{"перенос по пробелу", "hello world", 5, []string{"hello", "world"}},
		{"длинное слово", "abcdefgh", 3, []string{"abc", "def", "gh"}},
		{"жёсткие переносы", "a\nb\nc", 10, []string{"a", "b", "c"}},
		{"пустая строка внутри", "a\n\nb", 10, []string{"a", "", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := []rune(c.in)
			got := segText(text, wrapText(text, c.w))
			if !eqStrings(got, c.want) {
				t.Fatalf("wrapText(%q, %d) = %q, ожидалось %q", c.in, c.w, got, c.want)
			}
		})
	}
}

// TestWrapTextOffsets проверяет, что выделение по абсолютным смещениям копирует
// ровно исходный текст (включая пробел в точке мягкого переноса).
func TestWrapTextOffsets(t *testing.T) {
	text := []rune("hello world")
	segs := wrapText(text, 5)
	// Первая строка — "hello" [0,5), вторая — "world" [6,11). Пробел на индексе 5
	// не попадает ни в одну строку, но входит в исходный текст.
	if segs[0].end != 5 || segs[1].start != 6 {
		t.Fatalf("границы сегментов неожиданны: %+v", segs)
	}
	// Выделение от начала до конца включает пропущенный пробел.
	if got := string(text[0:11]); got != "hello world" {
		t.Fatalf("извлечение всего текста = %q", got)
	}
}

// TestCopyViewLocate проверяет соответствие смещение↔(строка,колонка).
func TestCopyViewLocate(t *testing.T) {
	c := &copyView{text: []rune("hello world"), anc: -1}
	c.rewrap(5)
	// pos=7 — это 'o' во втором слове (строка 1, колонка 1).
	if line, col := c.locate(7); line != 1 || col != 1 {
		t.Fatalf("locate(7) = (%d,%d), ожидалось (1,1)", line, col)
	}
	// Обратное преобразование.
	if p := c.posAt(1, 1); p != 7 {
		t.Fatalf("posAt(1,1) = %d, ожидалось 7", p)
	}
}

package cli

import (
	"encoding/json"
	"os"
	"time"
)

// printJSON печатает значение как форматированный JSON в stdout.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// formatTime форматирует время в локальной зоне; пустое время — пустая строка.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

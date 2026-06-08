package daemon

import (
	"os/exec"

	"github.com/cultivateweb/tgcli/internal/ipc"
)

// notify шлёт desktop-уведомление о входящем сообщении через notify-send.
// Если notify-send недоступен, тихо ничего не делает — уведомления необязательны.
func notify(ev ipc.MessageEvent) {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return
	}

	title := ev.From
	if title == "" {
		title = "tgcli"
	}

	body := ev.Text
	if ev.Media != "" {
		if body != "" {
			body += " "
		}
		body += "📎 " + ev.Media
	}
	body = truncate(body, 200)

	// -a задаёт имя приложения; запускаем неблокирующе и не ждём завершения.
	_ = exec.Command(path, "-a", "tgcli", title, body).Start()
}

// truncate ограничивает строку n рунами, добавляя многоточие при обрезке.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

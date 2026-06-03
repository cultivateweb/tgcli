package tui

// Диагностика зависаний. Фриз TUI воспроизводится только в живом запуске, а не
// в тестах, поэтому приложение само ловит «залипание» событийного цикла и пишет
// стек всех горутин в файл — по нему видно, какая горутина кого ждёт.

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// dumpStacks пишет стеки всех горутин в /tmp/tgcli-freeze-<unix>.log.
func dumpStacks(reason string) string {
	buf := make([]byte, 8<<20)
	n := runtime.Stack(buf, true)
	path := filepath.Join(os.TempDir(), fmt.Sprintf("tgcli-freeze-%d.log", time.Now().Unix()))
	body := append([]byte("причина: "+reason+"\nвремя: "+time.Now().Format(time.RFC3339)+"\n\n"), buf[:n]...)
	_ = os.WriteFile(path, body, 0o644)
	fmt.Fprintln(os.Stderr, "tgcli: снят дамп горутин →", path)
	return path
}

// startDiagnostics запускает сторож зависаний и обработчик SIGUSR1. Оба пишут
// дамп горутин в /tmp/tgcli-freeze-*.log. Сторож раз в 3 с пингует событийный
// цикл; если тот не отвечает дольше 6 с — фиксирует дамп (один раз за зависание).
// Ручной снимок без убийства процесса: kill -USR1 <pid> из другого терминала.
func (u *ui) startDiagnostics() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1)
	go func() {
		for range sig {
			dumpStacks("SIGUSR1 (ручной снимок)")
		}
	}()

	go func() {
		dumped := false
		for {
			time.Sleep(3 * time.Second)
			done := make(chan struct{})
			go u.app.QueueUpdate(func() { close(done) })
			select {
			case <-done:
				dumped = false
			case <-time.After(6 * time.Second):
				if !dumped {
					dumpStacks("сторож: событийный цикл не отвечает дольше 6 с")
					dumped = true
				}
			}
		}
	}()
}

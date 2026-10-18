package supervisor

import (
	"testing"

	"corepanel/internal/installer"
	"corepanel/internal/model"
	"corepanel/internal/platform"
)

// noteAwgLine считает серию неудачных рукопожатий и сбрасывает её после успешного.
// Сам перезапуск в этом тесте не запускается: ядро не работает, а noteAwgLine проверяет это.
func TestRecoveryCountsFails(t *testing.T) {
	info := platform.Info{OS: platform.Linux, DataDir: t.TempDir()}
	set := model.Default()
	set.Normalize()
	set.Recovery.Enabled = true
	sup := New(info, installer.New(info), func() *model.Settings { return set })

	line := func(what string) string {
		return "+0000 2026-10-18 12:00:00 DEBUG endpoint/awg[test]: peer(abc) - " + what
	}
	fails := func() int {
		sup.mu.Lock()
		defer sup.mu.Unlock()
		return sup.awgFails["test"]
	}

	sup.noteAwgLine(line("sending handshake initiation"))
	if got := fails(); got != 0 {
		t.Fatalf("попытка рукопожатия не считается неудачей, получено %d", got)
	}
	sup.noteAwgLine(line("handshake did not complete after 5 seconds, retrying (try 1)"))
	sup.noteAwgLine(line("handshake did not complete after 5 seconds, retrying (try 2)"))
	if got := fails(); got != 2 {
		t.Fatalf("серия неудач: получено %d, ожидалось 2", got)
	}
	sup.noteAwgLine(line("received handshake response"))
	if got := fails(); got != 0 {
		t.Fatalf("успешное рукопожатие должно сбрасывать серию, получено %d", got)
	}

	// выключенное слежение счётчики не копит
	set.Recovery.Enabled = false
	sup.noteAwgLine(line("handshake did not complete after 5 seconds, retrying (try 1)"))
	sup.mu.Lock()
	m := sup.awgFails
	sup.mu.Unlock()
	if m != nil {
		t.Fatalf("при выключенном слежении счётчики должны быть пусты, получено %v", m)
	}
}

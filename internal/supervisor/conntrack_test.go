package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withConntrack(t *testing.T, count, max string) string {
	t.Helper()
	dir := t.TempDir()
	old := conntrackDir
	conntrackDir = dir
	t.Cleanup(func() { conntrackDir = old })
	if count != "" {
		if err := os.WriteFile(filepath.Join(dir, "nf_conntrack_count"), []byte(count+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if max != "" {
		if err := os.WriteFile(filepath.Join(dir, "nf_conntrack_max"), []byte(max+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestApplyConntrackMax(t *testing.T) {
	if msg, err := applyConntrackMax(0); msg != "" || err != nil {
		t.Errorf("0 — ничего не делаем: %q %v", msg, err)
	}
	dir := withConntrack(t, "10", "16384")
	msg, err := applyConntrackMax(32768)
	if err != nil || !strings.Contains(msg, "16384 → 32768") {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "nf_conntrack_max")); strings.TrimSpace(string(b)) != "32768" {
		t.Errorf("записано %q", b)
	}
	if msg, err := applyConntrackMax(32768); msg != "" || err != nil {
		t.Errorf("то же значение — сообщения быть не должно: %q %v", msg, err)
	}
}

func TestApplyConntrackMaxErrors(t *testing.T) {
	withConntrack(t, "", "") // файлов нет
	if _, err := applyConntrackMax(32768); err == nil || !strings.Contains(err.Error(), "недоступна") {
		t.Errorf("ждали ошибку про недоступную таблицу: %v", err)
	}
	withConntrack(t, "", "мусор")
	if _, err := applyConntrackMax(32768); err == nil {
		t.Error("нечисловое значение должно давать ошибку")
	}
}

func TestConntrackNearFull(t *testing.T) {
	for _, c := range []struct {
		count, max int
		want       bool
	}{{89, 100, false}, {90, 100, true}, {16384, 16384, true}, {5, 0, false}, {0, 100, false}} {
		if got := conntrackNearFull(c.count, c.max); got != c.want {
			t.Errorf("%d/%d: %v", c.count, c.max, got)
		}
	}
	withConntrack(t, "15000", "16384")
	count, max, err := conntrackLoad()
	if err != nil || count != 15000 || max != 16384 {
		t.Errorf("load: %d %d %v", count, max, err)
	}
}

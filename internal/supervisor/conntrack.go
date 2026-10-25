package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// conntrackDir — каталог sysctl таблицы соединений; переменная, чтобы тесты подменяли его временным.
var conntrackDir = "/proc/sys/net/netfilter"

const (
	conntrackCheckEvery = 30 * time.Second
	conntrackWarnEvery  = 5 * time.Minute
)

func readSysInt(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return n, nil
}

// applyConntrackMax выставляет nf_conntrack_max и проверяет, что значение действительно принято.
// want <= 0 — ничего не делать. Возвращает сообщение для журнала (пустое, если менять было нечего).
func applyConntrackMax(want int) (string, error) {
	if want <= 0 {
		return "", nil
	}
	path := filepath.Join(conntrackDir, "nf_conntrack_max")
	cur, err := readSysInt(path)
	if err != nil {
		return "", fmt.Errorf("таблица соединений недоступна (модуль nf_conntrack не загружен?): %w", err)
	}
	if cur == want {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(want)), 0o644); err != nil {
		return "", fmt.Errorf("не удалось записать %d в %s: %w", want, path, err)
	}
	got, err := readSysInt(path)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("ядро ОС приняло %d вместо %d", got, want)
	}
	return fmt.Sprintf("предел conntrack: %d → %d", cur, want), nil
}

// conntrackLoad возвращает текущее число записей и предел таблицы соединений.
func conntrackLoad() (count, max int, err error) {
	if count, err = readSysInt(filepath.Join(conntrackDir, "nf_conntrack_count")); err != nil {
		return 0, 0, err
	}
	if max, err = readSysInt(filepath.Join(conntrackDir, "nf_conntrack_max")); err != nil {
		return 0, 0, err
	}
	return count, max, nil
}

// conntrackNearFull — таблица заполнена на 90% и больше.
func conntrackNearFull(count, max int) bool {
	return max > 0 && count*100 >= max*90
}

// watchConntrack, пока ядро работает, предупреждает в журнале панели о почти заполненной таблице соединений:
// при переполнении роутер отбрасывает пакеты, а процессор уходит на повторные попытки клиентов.
func (s *Supervisor) watchConntrack(done <-chan struct{}) {
	if _, _, err := conntrackLoad(); err != nil {
		return // на этой системе conntrack не виден — нечего проверять
	}
	tk := time.NewTicker(conntrackCheckEvery)
	defer tk.Stop()
	var last time.Time
	for {
		select {
		case <-done:
			return
		case <-tk.C:
		}
		count, max, err := conntrackLoad()
		if err != nil || !conntrackNearFull(count, max) {
			continue
		}
		if time.Since(last) < conntrackWarnEvery {
			continue
		}
		last = time.Now()
		s.Logs.Add(fmt.Sprintf("[panel] таблица соединений заполнена: %d из %d. Включите «Блокировать QUIC» в «Маршрутизации» или поднимите «Предел таблицы соединений» в «Сети»", count, max))
	}
}

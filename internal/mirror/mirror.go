// Package mirror — зеркала GitHub: список префиксов, перебор при загрузке и проверка доступности.
package mirror

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"corepanel/internal/dlnet"
	"corepanel/internal/model"
)

// Popular — публичные зеркала-префиксы для кнопки «Добавить популярные». Они сторонние и могут
// пропасть или перестать работать, поэтому в панели их всегда можно проверить.
var Popular = []string{
	"https://ghfast.top/",
	"https://gh-proxy.com/",
	"https://ghproxy.net/",
	"https://github.moeyy.xyz/",
	"https://hub.gitmirror.com/",
}

// Normalize приводит один адрес зеркала к виду «https://host/path» без пробелов и без хвостового «/».
func Normalize(s string) string { return strings.TrimRight(strings.TrimSpace(s), "/") }

// Valid сообщает, что адрес зеркала — корректный http(s)-URL.
func Valid(s string) bool {
	u, err := url.Parse(Normalize(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// List — зеркала в порядке перебора: сначала «основное» (старое поле Mirror), затем список Mirrors; без повторов и пустых.
func List(d model.Download) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range append([]string{d.Mirror}, d.Mirrors...) {
		m = Normalize(m)
		if m == "" || seen[strings.ToLower(m)] {
			continue
		}
		seen[strings.ToLower(m)] = true
		out = append(out, m)
	}
	return out
}

// IsGitHub — адрес принадлежит GitHub и может идти через зеркало-префикс.
func IsGitHub(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "raw.githubusercontent.com", "gist.githubusercontent.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	}
	return false
}

// Apply добавляет префикс зеркала перед адресом.
func Apply(prefix, raw string) string { return Normalize(prefix) + "/" + raw }

// Variants — адреса, которые нужно перебрать по очереди: каждое зеркало, затем прямой адрес.
// Для адресов не с GitHub и без зеркал остаётся только прямой.
func Variants(mirrors []string, raw string) []string {
	if !IsGitHub(raw) || len(mirrors) == 0 {
		return []string{raw}
	}
	out := make([]string, 0, len(mirrors)+1)
	for _, m := range mirrors {
		out = append(out, Apply(m, raw))
	}
	return append(out, raw)
}

// Host — имя узла адреса для сообщений пользователю («ghfast.top», «GitHub»).
func Host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	// адрес вида https://зеркало/https://github.com/... — показываем зеркало
	return u.Hostname()
}

// Result — результат проверки одного зеркала.
type Result struct {
	Mirror string `json:"mirror"` // пусто — прямой доступ к GitHub
	OK     bool   `json:"ok"`
	Ms     int64  `json:"ms"`
	Error  string `json:"error,omitempty"`
}

// probeURL — небольшой файл в репозитории, по нему проверяем, что зеркало отдаёт именно содержимое GitHub.
const probeURL = "https://raw.githubusercontent.com/phenomenonRT/amnezia-box/main/README.md"

// Check проверяет прямой доступ и каждое зеркало параллельно. Первой строкой идёт прямой доступ.
func Check(ctx context.Context, d model.Download, mirrors []string) []Result {
	cl := &http.Client{Timeout: 10 * time.Second}
	if tr, err := dlnet.Transport(d); err == nil {
		cl.Transport = tr
	}
	targets := append([]string{""}, mirrors...)
	res := make([]Result, len(targets))
	var wg sync.WaitGroup
	for i, m := range targets {
		wg.Add(1)
		go func(i int, m string) {
			defer wg.Done()
			res[i] = probe(ctx, cl, m)
		}(i, m)
	}
	wg.Wait()
	return res
}

func probe(ctx context.Context, cl *http.Client, m string) Result {
	r := Result{Mirror: m}
	u := probeURL
	if m != "" {
		u = Apply(m, probeURL)
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		r.Error = "неверный адрес"
		return r
	}
	req.Header.Set("User-Agent", "corepanel")
	resp, err := cl.Do(req)
	if err != nil {
		r.Error = clean(err)
		return r
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	r.Ms = time.Since(start).Milliseconds()
	switch {
	case resp.StatusCode != http.StatusOK:
		r.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	case len(b) == 0:
		r.Error = "пустой ответ"
	case strings.Contains(strings.ToLower(string(b[:min(len(b), 200)])), "<html"):
		r.Error = "вместо файла отдаётся веб-страница"
	default:
		r.OK = true
	}
	return r
}

func clean(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return "имя не определяется (DNS)"
	case strings.Contains(s, "deadline exceeded"), strings.Contains(s, "Timeout"), strings.Contains(s, "timeout"):
		return "нет ответа за 10 с"
	case strings.Contains(s, "refused"):
		return "соединение отклонено"
	case strings.Contains(s, "certificate"):
		return "ошибка сертификата"
	}
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

// Sorted возвращает копию результатов: рабочие впереди, по возрастанию времени ответа.
func Sorted(in []Result) []Result {
	out := append([]Result(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OK != out[j].OK {
			return out[i].OK
		}
		return out[i].Ms < out[j].Ms
	})
	return out
}

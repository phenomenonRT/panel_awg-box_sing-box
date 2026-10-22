// Package rulesets готовит локальные копии наборов правил для sing-box.
// Текстовые списки панель преобразует в JSON; наборы с интервалом 0 загружаются
// один раз и используются локально, чтобы ядро не обновляло их самостоятельно.
package rulesets

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"corepanel/internal/mirror"
	"corepanel/internal/model"
)

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._!@-]`)

// File — путь преобразованного текстового набора относительно рабочего каталога ядра.
func File(name string) string { return "rulesets/" + unsafe.ReplaceAllString(name, "_") + ".json" }

// LocalFile — путь к локальной копии набора с подходящим расширением.
func LocalFile(rs model.RuleSet) string {
	if IsText(rs) {
		return File(rs.Name)
	}
	ext := ".json"
	format := rs.Format
	if format == "" {
		if u, err := url.Parse(rs.URL); err == nil {
			format = strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), ".")
		}
	}
	if format == "srs" {
		ext = ".srs"
	}
	return "rulesets/" + unsafe.ReplaceAllString(rs.Name, "_") + ext
}

// IsText говорит, нужно ли набору преобразование: явно выбранный формат text или файл .txt/.list/.lst.
func IsText(rs model.RuleSet) bool {
	if rs.Format != "" {
		return rs.Format == "text"
	}
	if u, err := url.Parse(rs.URL); err == nil {
		switch strings.ToLower(path.Ext(u.Path)) {
		case ".txt", ".list", ".lst":
			return true
		}
	}
	return false
}

var domainRe = regexp.MustCompile(`^[A-Za-z0-9_*.-]+$`)

// Convert превращает текстовый список в исходный JSON-набор sing-box и возвращает число записей.
// Понимает: домен в строке (подразумевается домен и все поддомены), IP и CIDR, а также префиксы
// full:/domain:/keyword:/regexp: и строки Clash вида DOMAIN-SUFFIX,example.com. Комментарии (#, //, ;) пропускаются.
func Convert(text string) ([]byte, int) {
	var suffix, full, keyword, regex, cidr []string
	seen := map[string]bool{}
	add := func(dst *[]string, v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		*dst = append(*dst, v)
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\xef\xbb\xbf"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		if i := strings.Index(line, " #"); i > 0 {
			line = strings.TrimSpace(line[:i])
		}
		if k, v, ok := strings.Cut(line, ","); ok { // Clash: DOMAIN-SUFFIX,example.com[,no-resolve]
			v, _, _ = strings.Cut(v, ",")
			switch strings.ToUpper(strings.TrimSpace(k)) {
			case "DOMAIN-SUFFIX":
				add(&suffix, strings.ToLower(v))
			case "DOMAIN":
				add(&full, strings.ToLower(v))
			case "DOMAIN-KEYWORD":
				add(&keyword, strings.ToLower(v))
			case "IP-CIDR", "IP-CIDR6":
				add(&cidr, v)
			}
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok && !strings.Contains(v, ":") && !strings.Contains(k, ".") { // префиксы v2ray
			v = strings.ToLower(strings.TrimSpace(v))
			switch strings.ToLower(k) {
			case "domain":
				add(&suffix, v)
			case "full":
				add(&full, v)
			case "keyword":
				add(&keyword, v)
			case "regexp":
				add(&regex, strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
			}
			continue
		}
		if _, err := netip.ParsePrefix(line); err == nil {
			add(&cidr, line)
			continue
		}
		if a, err := netip.ParseAddr(line); err == nil {
			add(&cidr, a.String()+map[bool]string{true: "/32", false: "/128"}[a.Is4()])
			continue
		}
		line = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(line, "*."), "."))
		if domainRe.MatchString(line) && strings.Contains(line, ".") {
			add(&suffix, line)
		}
	}
	rule := map[string]any{}
	set := func(k string, v []string) {
		if len(v) > 0 {
			rule[k] = v
		}
	}
	set("domain", full)
	set("domain_suffix", suffix)
	set("domain_keyword", keyword)
	set("domain_regex", regex)
	set("ip_cidr", cidr)
	n := len(full) + len(suffix) + len(keyword) + len(regex) + len(cidr)
	rules := []any{}
	if n > 0 {
		rules = append(rules, rule)
	}
	b, _ := json.Marshal(map[string]any{"version": 2, "rules": rules})
	return b, n
}

// Fetcher скачивает адрес целиком.
type Fetcher func(ctx context.Context, rawURL string) ([]byte, error)

// NewFetcher — скачивание с GitHub: зеркала по порядку, затем напрямую, затем через локальный прокси ядра.
func NewFetcher(mirrors []string, proxyPort int) Fetcher {
	get := func(ctx context.Context, u string, proxy *url.URL) ([]byte, error) {
		tr := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 20 * time.Second}
		if proxy != nil {
			tr.Proxy = http.ProxyURL(proxy)
		}
		cl := &http.Client{Transport: tr, Timeout: 45 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := cl.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	}
	return func(ctx context.Context, raw string) ([]byte, error) {
		var b []byte
		var err error
		for _, u := range mirror.Variants(mirrors, raw) {
			if b, err = get(ctx, u, nil); err == nil {
				return b, nil
			}
		}
		if proxyPort <= 0 {
			return b, err
		}
		if p, perr := url.Parse(fmt.Sprintf("http://%s", net.JoinHostPort("127.0.0.1", fmt.Sprint(proxyPort)))); perr == nil {
			if b2, err2 := get(ctx, raw, p); err2 == nil {
				return b2, nil
			}
		}
		return nil, err
	}
}

// Prepare скачивает и преобразует текстовые наборы в dir/rulesets. Свежий файл (младше интервала, тот же адрес)
// не трогает, пока force=false. Возвращает предупреждения; при ошибке без старой копии пишет пустой набор,
// чтобы ядро всё равно запустилось.
func Prepare(ctx context.Context, dir string, sets []model.RuleSet, fetch Fetcher, force bool) []string {
	return PrepareWithRAM(ctx, dir, "", sets, fetch, force)
}

// PrepareWithRAM хранит базовую копию текстового набора в dir/rulesets, а рабочую копию и метку обновления —
// в ramDir. Если RAM-копия пропала после перезагрузки, восстанавливает её из базы и сразу проверяет источник.
func PrepareWithRAM(ctx context.Context, dir, ramDir string, sets []model.RuleSet, fetch Fetcher, force bool) []string {
	var warns []string
	for _, rs := range sets {
		textSet := IsText(rs)
		if (!textSet && rs.Interval > 0) || rs.URL == "" {
			continue
		}
		file := filepath.Join(dir, filepath.FromSlash(LocalFile(rs)))
		activeFile, stamp := file, file+".src"
		coldRAM := false
		if ramDir != "" {
			activeFile = filepath.Join(ramDir, filepath.FromSlash(LocalFile(rs)))
			stamp = activeFile + ".src"
			if _, err := os.Stat(activeFile); err != nil {
				if !os.IsNotExist(err) {
					warns = append(warns, fmt.Sprintf("набор «%s»: %v", rs.Name, err))
					continue
				}
				coldRAM = true
				if _, err := os.Stat(file); err == nil {
					if err := copyIfChanged(file, activeFile); err != nil {
						warns = append(warns, fmt.Sprintf("набор «%s»: не удалось восстановить копию в RAM: %v", rs.Name, err))
						continue
					}
				} else if !os.IsNotExist(err) {
					warns = append(warns, fmt.Sprintf("набор «%s»: %v", rs.Name, err))
					continue
				}
			}
		}
		iv := time.Duration(rs.Interval) * time.Second
		if iv <= 0 && !force {
			if sourceMatches(stamp, rs.URL) || hasPersistentBaseline(file, rs.URL) {
				continue
			}
		}
		if iv > 0 && !force && !coldRAM {
			if st, err := os.Stat(stamp); err == nil && time.Since(st.ModTime()) < iv {
				if old, _ := os.ReadFile(stamp); string(old) == rs.URL {
					continue
				}
			}
		}
		if err := os.MkdirAll(filepath.Dir(activeFile), 0o700); err != nil {
			warns = append(warns, fmt.Sprintf("набор «%s»: %v", rs.Name, err))
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		body, err := fetch(cctx, rs.URL)
		cancel()
		if err != nil {
			warns = append(warns, fmt.Sprintf("набор «%s»: не удалось скачать %s: %v", rs.Name, rs.URL, err))
			if textSet {
				if _, e := os.Stat(activeFile); !os.IsNotExist(e) {
					continue
				}
				if writeErr := writeIfChanged(activeFile, []byte(`{"version":2,"rules":[]}`)); writeErr != nil {
					warns = append(warns, fmt.Sprintf("набор «%s»: не удалось создать пустую копию: %v", rs.Name, writeErr))
				}
			}
			continue
		}
		content := body
		if textSet {
			var n int
			content, n = Convert(string(body))
			if n == 0 {
				warns = append(warns, fmt.Sprintf("набор «%s»: в списке %s не нашлось ни одного домена или подсети", rs.Name, rs.URL))
				if _, err := os.Stat(activeFile); err == nil {
					continue
				}
			}
		} else if filepath.Ext(file) == ".json" && !json.Valid(body) {
			warns = append(warns, fmt.Sprintf("набор «%s»: источник %s вернул некорректный JSON; оставлена старая версия", rs.Name, rs.URL))
			continue
		} else if filepath.Ext(file) == ".srs" && (len(body) < 4 || !bytes.HasPrefix(body, []byte("SRS"))) {
			warns = append(warns, fmt.Sprintf("набор «%s»: источник %s вернул некорректный SRS; оставлена старая версия", rs.Name, rs.URL))
			continue
		}
		if ramDir != "" && !hasPersistentBaseline(file, rs.URL) {
			if err := writeIfChanged(file, content); err != nil {
				warns = append(warns, fmt.Sprintf("набор «%s»: не удалось сохранить первую копию во флеш-память: %v", rs.Name, err))
				continue
			}
			if err := writeIfChanged(file+".src", []byte(rs.URL)); err != nil {
				warns = append(warns, fmt.Sprintf("набор «%s»: не удалось сохранить адрес базовой копии: %v", rs.Name, err))
			}
		}
		if err := writeIfChanged(activeFile, content); err != nil {
			warns = append(warns, fmt.Sprintf("набор «%s»: не удалось сохранить список: %v", rs.Name, err))
			continue
		}
		// В режиме RAM метка тоже временная: после перезагрузки источник проверится повторно.
		markFetched(stamp, rs.URL)
	}
	return warns
}

func writeIfChanged(file string, data []byte) error {
	if old, err := os.ReadFile(file); err == nil {
		if bytes.Equal(old, data) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func copyIfChanged(source, dest string) error {
	b, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeIfChanged(dest, b)
}

func hasPersistentBaseline(file, sourceURL string) bool {
	if _, err := os.Stat(file); err != nil {
		return false
	}
	old, err := os.ReadFile(file + ".src")
	return err == nil && string(old) == sourceURL
}

func sourceMatches(stamp, sourceURL string) bool {
	old, err := os.ReadFile(stamp)
	return err == nil && string(old) == sourceURL
}

func markFetched(stamp, sourceURL string) {
	if old, err := os.ReadFile(stamp); err == nil && string(old) == sourceURL {
		now := time.Now()
		_ = os.Chtimes(stamp, now, now)
		return
	}
	_ = os.WriteFile(stamp, []byte(sourceURL), 0o600)
}

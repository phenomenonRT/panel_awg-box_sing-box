package gen

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var autoHostRe = regexp.MustCompile(`^(?:[\p{L}\p{N}_](?:[\p{L}\p{N}_-]{0,61}[\p{L}\p{N}_])?\.)+[\p{L}\p{N}-]{2,63}$`)

// autoRuleTypes — порядок, в котором результат автоопределения превращается в правила.
var autoRuleTypes = []string{"domain", "domain_suffix", "domain_keyword", "domain_regex", "ip_cidr"}

// classifyAuto раскладывает значения правила «Автоматически» по типам sing-box:
// IP и подсети → ip_cidr, домен (и *.домен) → domain_suffix, full:домен → domain,
// keyword:слово → domain_keyword, regexp:выражение → domain_regex, ссылка → её хост.
// Нераспознанные значения возвращаются отдельно.
func classifyAuto(vals []string) (map[string][]string, []string) {
	groups := map[string][]string{}
	var bad []string
	seen := map[string]bool{}
	add := func(t, v string) {
		k := t + "\x00" + v
		if !seen[k] {
			seen[k] = true
			groups[t] = append(groups[t], v)
		}
	}
	for _, raw := range vals {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], ".") && !strings.Contains(s, "::") && !strings.Contains(s, "://") {
			v := strings.TrimSpace(s[i+1:])
			switch strings.ToLower(s[:i]) {
			case "full":
				if v != "" {
					add("domain", strings.ToLower(v))
					continue
				}
			case "keyword":
				if v != "" {
					add("domain_keyword", strings.ToLower(v))
					continue
				}
			case "regexp", "regex":
				if _, err := regexp.Compile(v); err == nil && v != "" {
					add("domain_regex", v)
					continue
				}
			case "domain", "suffix":
				s = v
			}
		}
		if strings.Contains(s, "://") {
			u, err := url.Parse(s)
			if err != nil || u.Hostname() == "" {
				bad = append(bad, raw)
				continue
			}
			s = u.Hostname()
		}
		s = strings.Trim(s, "[]")
		if ip := net.ParseIP(s); ip != nil {
			add("ip_cidr", ip.String())
			continue
		}
		if _, n, err := net.ParseCIDR(s); err == nil {
			add("ip_cidr", n.String())
			continue
		}
		d := strings.ToLower(strings.TrimSuffix(strings.TrimLeft(s, "*."), "."))
		if autoHostRe.MatchString(d) {
			add("domain_suffix", d)
			continue
		}
		bad = append(bad, raw)
	}
	return groups, bad
}

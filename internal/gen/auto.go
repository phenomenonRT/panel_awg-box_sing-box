package gen

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var autoHostRe = regexp.MustCompile(`^(?:[\p{L}\p{N}_](?:[\p{L}\p{N}_-]{0,61}[\p{L}\p{N}_])?\.)+[\p{L}\p{N}-]{2,63}$`)

// autoRuleTypes — порядок, в котором результат автоопределения превращается в правила.
var autoRuleTypes = []string{"domain_suffix", "ip_cidr"}

// classifyAuto раскладывает значения правила «Автоматически» по типам sing-box:
// IP и подсети → ip_cidr, домен (вместе с поддоменами) → domain_suffix, ссылка → её домен.
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
		d := strings.ToLower(strings.TrimSuffix(s, "."))
		if autoHostRe.MatchString(d) {
			add("domain_suffix", d)
			continue
		}
		bad = append(bad, raw)
	}
	return groups, bad
}

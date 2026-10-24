package gen

import (
	"encoding/json"
	"reflect"
	"testing"

	"corepanel/internal/model"
)

func TestClassifyAuto(t *testing.T) {
	g, bad := classifyAuto([]string{"example.com", "YouTube.com", "example.com", "1.2.3.4", "10.1.2.3/8", "2001:db8::/32",
		"https://www.site.org/a/b", "999.1.1.1", "oops", "*.bad.com", "full:x.com"})
	want := map[string][]string{
		"domain_suffix": {"example.com", "youtube.com", "www.site.org"},
		"ip_cidr":       {"1.2.3.4", "10.0.0.0/8", "2001:db8::/32"},
	}
	if !reflect.DeepEqual(g, want) {
		t.Errorf("группы: %v", g)
	}
	if len(bad) != 4 {
		t.Errorf("не распознано: %v", bad)
	}
}

func TestAutoRuleInRoute(t *testing.T) {
	s := sample()
	s.Rules = append(s.Rules, model.Rule{Type: "auto", Values: []string{"example.org", "203.0.113.5", "198.51.100.0/24"}, Outbound: "proxy"})
	res, _ := Singbox(s)
	var m map[string]any
	if err := json.Unmarshal([]byte(res.Config), &m); err != nil {
		t.Fatal(err)
	}
	var sawDomain, sawIP bool
	for _, r := range m["route"].(map[string]any)["rules"].([]any) {
		rm := r.(map[string]any)
		if v, ok := rm["domain_suffix"].([]any); ok && len(v) == 1 && v[0] == "example.org" {
			sawDomain = true
		}
		if v, ok := rm["ip_cidr"].([]any); ok && len(v) == 2 && v[0] == "203.0.113.5" && rm["outbound"] == "proxy" {
			sawIP = true
		}
	}
	if !sawDomain || !sawIP {
		t.Errorf("domain=%v ip=%v", sawDomain, sawIP)
	}
}

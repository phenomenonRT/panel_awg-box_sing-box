package gen

import (
	"encoding/json"
	"testing"
)

func TestBlockQUICBeforeSniff(t *testing.T) {
	find := func(block bool) (quic, sniff int) {
		s := sample()
		s.General.Sniff = true
		s.General.BlockQUIC = block
		res, _ := Singbox(s)
		var m map[string]any
		if err := json.Unmarshal([]byte(res.Config), &m); err != nil {
			t.Fatal(err)
		}
		quic, sniff = -1, -1
		for i, r := range m["route"].(map[string]any)["rules"].([]any) {
			rm := r.(map[string]any)
			if rm["action"] == "sniff" && sniff < 0 {
				sniff = i
			}
			if rm["action"] == "reject" && rm["network"] != nil {
				if n, ok := rm["network"].([]any); ok && len(n) == 1 && n[0] == "udp" {
					if p, ok := rm["port"].([]any); ok && len(p) == 1 && p[0] == float64(443) {
						quic = i
					}
				}
			}
		}
		return
	}
	if q, _ := find(false); q != -1 {
		t.Errorf("правило QUIC не должно быть без опции")
	}
	q, sn := find(true)
	if q < 0 || sn < 0 || q > sn {
		t.Errorf("QUIC-правило должно стоять до sniff: quic=%d sniff=%d", q, sn)
	}
}

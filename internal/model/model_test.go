package model

import "testing"

func TestNormalizeMigratesASNHost(t *testing.T) {
	s := Default()
	s.RuleSets = []RuleSet{{Name: "asn-aws-v4", URL: "https://asn.web2core.workers.dev/AS16509,AS14618?v4", Format: "text"}}
	s.Normalize()
	if got := s.RuleSets[0].URL; got != "https://sw.ext.io/AS16509,AS14618?v4" {
		t.Fatalf("URL не перенесён: %s", got)
	}
}

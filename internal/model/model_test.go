package model

import "testing"

func TestNormalizeMigratesASNHost(t *testing.T) {
	s := Default()
	s.RuleSets = []RuleSet{{Name: "asn-aws-v4", URL: "https://asn.web2core.workers.dev/AS16509,AS14618?v4", Format: "text"}}
	s.Normalize()
	if got := s.RuleSets[0].URL; got != "https://sw.ext.io/asw/AS16509,AS14618?v4" {
		t.Fatalf("URL не перенесён: %s", got)
	}
	s.RuleSets[0].URL = "https://sw.ext.io/AS50340?v4" // адрес из версии 0.1.92
	s.Normalize()
	if got := s.RuleSets[0].URL; got != "https://sw.ext.io/asw/AS50340?v4" {
		t.Fatalf("адрес без /asw/ не исправлен: %s", got)
	}
	s.Normalize()
	if got := s.RuleSets[0].URL; got != "https://sw.ext.io/asw/AS50340?v4" {
		t.Fatalf("повторная нормализация испортила адрес: %s", got)
	}
}

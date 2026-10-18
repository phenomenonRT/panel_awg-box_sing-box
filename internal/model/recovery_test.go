package model

import "testing"

func TestRecoveryNormalize(t *testing.T) {
	s := Default()
	s.Recovery.FailThreshold = 0
	s.Recovery.MaxRestarts = 0
	s.Normalize()
	if s.Recovery.FailThreshold != 5 || s.Recovery.MaxRestarts != 3 {
		t.Fatalf("значения по умолчанию: %+v", s.Recovery)
	}

	s.Recovery.FailThreshold = 1000
	s.Recovery.MaxRestarts = -1
	s.Normalize()
	if s.Recovery.FailThreshold != RecoveryFailsMax || s.Recovery.MaxRestarts != RecoveryRestartsMin {
		t.Fatalf("пределы не применились: %+v", s.Recovery)
	}
}

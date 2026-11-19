package main

import "testing"

func TestBuildVersionPrefersTheInjectedValue(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "v9.9.9"
	if got := buildVersion(); got != "v9.9.9" {
		t.Fatalf("version=%q", got)
	}
}

func TestBuildVersionNeverReturnsEmpty(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "dev"
	if buildVersion() == "" {
		t.Fatal("empty version")
	}
}

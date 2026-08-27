package gptcheck

import "testing"

func TestParseSessionTokenJSON(t *testing.T) {
	got := parseSessionToken(`{"sessionToken":"a.b.c.d.e","accessToken":"x"}`)
	if got != "a.b.c.d.e" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSessionTokenJWE(t *testing.T) {
	raw := "eyJ.aaa.bbb.ccc.ddd"
	if got := parseSessionToken(raw); got != raw {
		t.Fatalf("got %q", got)
	}
}

func TestRefreshSessionMissingToken(t *testing.T) {
	if _, err := RefreshSession(`{"accessToken":"eyJ"}`); err == nil {
		t.Fatal("expected error")
	}
}

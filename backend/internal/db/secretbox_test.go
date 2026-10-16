package db

import (
	"strings"
	"testing"
)

func TestSealSecretRoundTrip(t *testing.T) {
	t.Setenv("SECRET_ENC_KEY", "unit-test-key")
	sealed, err := SealSecret(`{"sessionToken":"abc"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, sealPrefix) || strings.Contains(sealed, "sessionToken") {
		t.Fatalf("not sealed: %q", sealed)
	}
	if again, _ := SealSecret(sealed); again != sealed {
		t.Fatal("double seal")
	}
	if got := OpenSecret(sealed); got != `{"sessionToken":"abc"}` {
		t.Fatalf("open = %q", got)
	}
	if got := OpenSecret("legacy-plain"); got != "legacy-plain" {
		t.Fatalf("plain passthrough = %q", got)
	}
	t.Setenv("SECRET_ENC_KEY", "other-key")
	if got := OpenSecret(sealed); got != "" {
		t.Fatalf("wrong key should yield empty, got %q", got)
	}
}

func TestCDKSessionStoredEncrypted(t *testing.T) {
	t.Setenv("SECRET_ENC_KEY", "unit-test-key")
	openTestDB(t)
	code := "SXC-SEAL-TEST-0001"
	sess := `{"sessionToken":"tok-123","user":{"email":"a@b.c"}}`
	if err := BindCDKSession(code, "rt-1", sess); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := DB.QueryRow(`SELECT session_payload FROM cdk_session_bindings WHERE cdk_code = ?`, code).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "tok-123") {
		t.Fatalf("session stored in plaintext: %q", raw)
	}
	if got, _ := GetSessionByCDK(code); got != sess {
		t.Fatalf("GetSessionByCDK = %q", got)
	}
	if b, _ := GetBindingByCDK(code); b == nil || b.SessionPayload != sess {
		t.Fatalf("GetBindingByCDK = %+v", b)
	}
}

func TestMigrateSealsLegacyPlaintext(t *testing.T) {
	t.Setenv("SECRET_ENC_KEY", "unit-test-key")
	openTestDB(t)
	if _, err := DB.Exec(`INSERT INTO cdk_session_bindings (cdk_code, session_payload, redemption_token) VALUES ('SXC-LEGACY-1', 'plain-session', 'rt')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateSealCDKSessions(); err != nil {
		t.Fatal(err)
	}
	var raw string
	_ = DB.QueryRow(`SELECT session_payload FROM cdk_session_bindings WHERE cdk_code = 'SXC-LEGACY-1'`).Scan(&raw)
	if !strings.HasPrefix(raw, sealPrefix) {
		t.Fatalf("legacy row not sealed: %q", raw)
	}
	if got, _ := GetSessionByCDK("SXC-LEGACY-1"); got != "plain-session" {
		t.Fatalf("read back = %q", got)
	}
}

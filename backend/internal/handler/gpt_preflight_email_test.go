package handler

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
)

func fakeAccessToken(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(claims)) + ".sig"
}

func TestPreflightAccountEmail(t *testing.T) {
	bare := fakeAccessToken(`{"https://api.openai.com/profile":{"email":"jwt@example.com"}}`)
	cases := []struct {
		name, raw, session, want string
	}{
		{"upstream wins", `{"data":{"account_email":"up@example.com"}}`, bare, "up@example.com"},
		{"bare access token", `{"ok":true}`, bare, "jwt@example.com"},
		{"session json", `{}`, `{"sessionToken":"st","user":{"email":"js@example.com"}}`, "js@example.com"},
		{"nothing", `{}`, "opaque", ""},
	}
	for _, tc := range cases {
		body := map[string]any{"credential": map[string]any{"mode": "session", "session": tc.session}}
		if got := preflightAccountEmail([]byte(tc.raw), body); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

// 裸 access token 不落库，但通知仍要能显示预检时识别到的邮箱。
func TestGPTNoticeUsesPreflightEmail(t *testing.T) {
	openHandlerTestDB(t)
	code := "ZC-PREFLIGHT-EMAIL"
	if err := db.SaveCardplatformCDKCode(101, code, "ZC", "plus", 100); err != nil {
		t.Fatal(err)
	}
	if err := db.BindCDKRedemptionToken(code, "tok"); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"credential": map[string]any{"mode": "session",
		"session": fakeAccessToken(`{"https://api.openai.com/profile":{"email":"buyer@example.com"}}`)}}
	if bindableSession(200, body) != "" {
		t.Fatal("bare access token must not be stored")
	}
	// 被拒的预检不能写邮箱。
	bindPreflightEmail(400, "", "tok", []byte(`{}`), body)
	if got, _ := db.GetEmailByCDK(code); got != "" {
		t.Fatalf("rejected preflight stored %q", got)
	}
	// 老卡台预检只带 redemption_token。
	bindPreflightEmail(200, "", "tok", []byte(`{}`), body)
	if got, _ := db.GetEmailByCDK(strings.ToLower(code)); got != "buyer@example.com" {
		t.Fatalf("stored email %q", got)
	}
	if sess, _ := db.GetSessionByCDK(code); sess != "" {
		t.Fatalf("session stored: %q", sess)
	}

	payload := gptTestPayload(t, `{"status":"completed","plan":"plus"}`)
	if err := notifyGPTSuccess(101, code, "", payload); err != nil {
		t.Fatal(err)
	}
	var text string
	if err := db.DB.QueryRow(`SELECT text FROM telegram_notifications`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "账号: buyer@example.com") {
		t.Fatalf("missing email in %s", text)
	}
}

// 换号重新预检、新号识别不到邮箱时，不能留着上一个号的邮箱。
func TestPreflightEmailOverwrittenOnRetry(t *testing.T) {
	openHandlerTestDB(t)
	code := "ZC-PREFLIGHT-RETRY"
	first := map[string]any{"credential": map[string]any{"session": `{"sessionToken":"a","user":{"email":"old@example.com"}}`}}
	bindPreflightEmail(200, code, "tok", []byte(`{}`), first)
	second := map[string]any{"credential": map[string]any{"session": "opaque"}}
	bindPreflightEmail(200, code, "tok", []byte(`{}`), second)
	if got, err := db.GetEmailByCDK(code); err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

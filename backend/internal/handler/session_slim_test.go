package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
)

// 仿 chatgpt.com/api/auth/session 新格式：正常字段 + 30 KB 的 marketingEligibility。
func bloatedSession(t *testing.T) string {
	t.Helper()
	big := map[string]any{"offers": strings.Repeat("x", 30<<10)}
	raw, err := json.Marshal(map[string]any{
		"WARNING_BANNER":       "DO NOT SHARE",
		"user":                 map[string]any{"email": "a@example.com"},
		"account":              map[string]any{"planType": "free"},
		"expires":              "2026-11-01T09:05:00Z",
		"accessToken":          "eyJa." + strings.Repeat("b", 2000) + ".c",
		"sessionToken":         strings.Repeat("s", 4300),
		"authProvider":         "openai",
		"statsigContext":       map[string]any{"k": "v"},
		"marketingEligibility": big,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSlimSessionJSON(t *testing.T) {
	raw := bloatedSession(t)
	if len(raw) <= maxBoundSessionBytes {
		t.Fatalf("fixture should exceed the limit, got %d", len(raw))
	}
	slim := slimSessionJSON(raw)
	if len(slim) > maxBoundSessionBytes {
		t.Fatalf("slim session still %d bytes", len(slim))
	}
	var o map[string]any
	if err := json.Unmarshal([]byte(slim), &o); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"user", "account", "expires", "accessToken", "sessionToken", "authProvider"} {
		if _, ok := o[k]; !ok {
			t.Fatalf("slim dropped %s", k)
		}
	}
	for _, k := range []string{"marketingEligibility", "WARNING_BANNER", "statsigContext"} {
		if _, ok := o[k]; ok {
			t.Fatalf("slim kept %s", k)
		}
	}
	// 账单/通知靠 session 取邮箱，精简后仍能取到
	if got := extractEmailFromSession(slim); got != "a@example.com" {
		t.Fatalf("email after slim %q", got)
	}
	// 不需要精简的、非 JSON 的、缺 sessionToken 的保持原文
	for _, in := range []string{precheckTestSession, "eyJa.b.c.d.e", `{"accessToken":"x","marketingEligibility":{}}`, "{bad"} {
		if got := slimSessionJSON(in); got != in {
			t.Fatalf("slimSessionJSON(%q) changed to %q", in, got)
		}
	}
}

// 新格式的大 Session 能通过校验，上游收到的是精简后的版本。
func TestPrecheckAcceptsBloatedSession(t *testing.T) {
	var sent string
	withPrecheckUpstream(t, func(_ context.Context, s string) (*cardplatform.DirectPreflightResult, error) {
		sent = s
		return &cardplatform.DirectPreflightResult{Email: "a@example.com", CurrentPlan: "free"}, nil
	})
	code, out := postPrecheck(t, bloatedSession(t))
	if code != 200 || out["valid"] != true {
		t.Fatalf("code=%d out=%v", code, out)
	}
	if strings.Contains(sent, "marketingEligibility") || !strings.Contains(sent, "sessionToken") {
		t.Fatalf("upstream got unslimmed session (%d bytes)", len(sent))
	}
	// 真正超大的请求仍然拒绝
	if code, _ := postPrecheck(t, `{"sessionToken":"`+strings.Repeat("s", maxRawSessionBytes)+`"}`); code != 400 {
		t.Fatalf("oversized raw session code=%d", code)
	}
}

// 兑换预检成功后绑定到卡密的是精简后的 Session，账单查询能用。
func TestBindableSessionSlimsBloatedSession(t *testing.T) {
	sess := bindableSession(200, map[string]any{"credential": map[string]any{"mode": "session", "session": bloatedSession(t)}})
	if sess == "" || strings.Contains(sess, "marketingEligibility") || len(sess) > maxBoundSessionBytes {
		t.Fatalf("bound session %d bytes", len(sess))
	}
}

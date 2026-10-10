package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danew/cdk-recharge-system/internal/config"
	"github.com/danew/cdk-recharge-system/internal/db"
)

const fakeToken = "123456:FAKE_TEST_TOKEN_ONLY"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	oldDB, oldClient := db.DB, httpClient
	db.DB = nil
	// Fail closed: no test may accidentally use a real HTTP transport.
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected HTTP request")
		return nil, errors.New("unexpected request")
	})}
	t.Cleanup(func() { db.DB, httpClient = oldDB, oldClient })
}

func queueDB(t *testing.T) {
	t.Helper()
	isolate(t)
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "notify.db"))
	t.Setenv("INSTALL_MODE", "wizard")
	if err := db.Init(&config.DatabaseConfig{}); err != nil {
		t.Fatal(err)
	}
	conn := db.DB
	t.Cleanup(func() { _ = conn.Close() })
}

func configure(t *testing.T) {
	t.Helper()
	t.Setenv("TELEGRAM_BOT_TOKEN", fakeToken)
	t.Setenv("TELEGRAM_CHAT_ID", "FAKE_CHAT")
}

func TestSendNowOffline(t *testing.T) {
	isolate(t)
	configure(t)
	cases := []struct {
		name string
		code int
		body string
		want string
	}{
		{"success", 200, `{"ok":true}`, ""},
		{"false", 200, `{"ok":false,"error_code":400,"description":"Bad chat"}`, "code 400: Bad chat"},
		{"missing ok", 200, `{}`, "HTTP 200"},
		{"malformed", 200, `<html>error</html>`, "invalid JSON"},
		{"malformed token echo", 200, `{"` + fakeToken, "invalid JSON"},
		{"oversized token echo", 200, strings.Repeat(fakeToken, 4096), "response too large"},
		{"wrong type", 200, `{"ok":"true"}`, "invalid JSON"},
		{"JSON field token echo", 200, fmt.Sprintf(`{"%s":{"ok":true},"error_code":"%s"}`, fakeToken, fakeToken), "invalid JSON"},
		{"non2xx", 500, `{"ok":true,"error_code":500,"description":"server error"}`, "HTTP 500"},
		{"http token echo", 401, fmt.Sprintf(`{"ok":false,"error_code":401,"description":"bad %s https://api.telegram.org/bot%s/sendMessage"}`, fakeToken, fakeToken), "code 401"},
		{"encoded token", 400, fmt.Sprintf(`{"ok":false,"description":"%s"}`, url.QueryEscape(fakeToken)), "redacted"},
		{"null", 200, `null`, "HTTP 200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "POST" || r.URL.Host != "api.telegram.org" {
					t.Fatal("wrong request")
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("text") != "hello" || r.Form.Get("parse_mode") != "HTML" {
					t.Fatal("wrong form")
				}
				return response(tc.code, tc.body), nil
			})
			err := SendNow("hello")
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want %q", err, tc.want)
			}
			assertSafe(t, err)
		})
	}
}

func assertSafe(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, secret := range []string{fakeToken, url.QueryEscape(fakeToken), "https://api.telegram.org"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("leaked credential or URL: %v", err)
		}
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errors.New("read failed " + fakeToken) }
func (brokenBody) Close() error             { return nil }

func TestTransportReadRedirectErrors(t *testing.T) {
	isolate(t)
	configure(t)
	httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network %s %s", r.URL, fakeToken)
	})
	assertSafe(t, SendNow("hello"))
	httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: brokenBody{}}, nil
	})
	assertSafe(t, SendNow("hello"))
	calls := 0
	httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		r := response(302, `{"ok":true}`)
		r.Header.Set("Location", "https://untrusted.invalid/bot"+fakeToken)
		return r, nil
	})
	assertSafe(t, SendNow("hello"))
	if calls != 1 {
		t.Fatalf("redirect followed: %d calls", calls)
	}
}

func state(t *testing.T, key string) (string, int, string) {
	t.Helper()
	var status, last string
	var attempts int
	if err := db.DB.QueryRow(`SELECT status, attempts, last_error FROM telegram_notifications WHERE business_key = ?`, key).Scan(&status, &attempts, &last); err != nil {
		t.Fatal(err)
	}
	return status, attempts, last
}

func due(t *testing.T, key string) {
	t.Helper()
	if _, err := db.DB.Exec(`UPDATE telegram_notifications SET next_attempt_at=0 WHERE business_key=?`, key); err != nil {
		t.Fatal(err)
	}
}

func TestQueueRetriesConfigRecoveryAndDedupe(t *testing.T) {
	queueDB(t)
	if err := EnqueueChatGPTRedeemed("event", "Plus", "person@example.test", "", "美区"); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueRedeemed("event", "X", "Premium", "different", "1"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	drain(context.Background())
	status, attempts, last := state(t, "event")
	if status != "retry" || attempts != 1 || last != "telegram not configured" {
		t.Fatalf("state %s %d %s", status, attempts, last)
	}
	drain(context.Background())
	_, attempts, _ = state(t, "event")
	if attempts != 1 {
		t.Fatal("retry ignored backoff")
	}
	configure(t)
	calls := 0
	httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(200, fmt.Sprintf(`{"ok":false,"description":"%s"}`, fakeToken)), nil
		}
		return response(200, `{"ok":true}`), nil
	})
	due(t, "event")
	drain(context.Background())
	status, attempts, last = state(t, "event")
	if status != "retry" || attempts != 2 || strings.Contains(last, fakeToken) {
		t.Fatalf("failure state %s %d %s", status, attempts, last)
	}
	due(t, "event")
	drain(context.Background())
	status, attempts, last = state(t, "event")
	if status != "sent" || attempts != 3 || last != "" {
		t.Fatalf("sent state %s %d %s", status, attempts, last)
	}
	drain(context.Background())
	if calls != 2 {
		t.Fatalf("unexpected send count %d", calls)
	}
}

func TestRestartExpiredLeaseAndCancellation(t *testing.T) {
	queueDB(t)
	configure(t)
	if err := EnqueueRedeemed("restart", "X", "Premium", "user", ""); err != nil {
		t.Fatal(err)
	}
	n, err := db.ClaimTelegramNotification(time.Now(), deliveryLease)
	if err != nil || n == nil {
		t.Fatalf("claim %v %v", n, err)
	}
	httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, `{"ok":true}`), nil })
	drain(context.Background())
	status, attempts, _ := state(t, "restart")
	if status != "sending" || attempts != 1 {
		t.Fatal("stole live lease")
	}
	// Model a process dying with its row leased, then a fresh process opening
	// the same persistent DB after lease expiry.
	if _, err := db.DB.Exec(`UPDATE telegram_notifications SET lease_until=0 WHERE business_key='restart'`); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Init(&config.DatabaseConfig{}); err != nil {
		t.Fatal(err)
	}
	reopened := db.DB
	t.Cleanup(func() { _ = reopened.Close() })
	drain(context.Background())
	status, attempts, _ = state(t, "restart")
	if status != "sent" || attempts != 2 {
		t.Fatalf("not recovered: %s %d", status, attempts)
	}
	if err := EnqueueRedeemed("cancel", "X", "P", "u", ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runWorker(ctx)
	status, attempts, _ = state(t, "cancel")
	if status != "pending" || attempts != 0 {
		t.Fatal("canceled worker claimed")
	}
}

func TestWorkerImmediateDrainAndStopsInFlight(t *testing.T) {
	queueDB(t)
	configure(t)
	for i := 0; i < 3; i++ {
		if err := EnqueueRedeemed(fmt.Sprint(i), "X", "P", "u", ""); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	started := make(chan struct{})
	httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	done := make(chan struct{})
	go func() { runWorker(ctx); close(done) }()
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("startup did not drain immediately")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker failed to stop")
	}
	if calls.Load() != 1 {
		t.Fatalf("claim avalanche: %d", calls.Load())
	}
	_, attempts, _ := state(t, "1")
	if attempts != 0 {
		t.Fatal("claimed next row after cancellation")
	}
}

func TestPureFormattingAndBackoff(t *testing.T) {
	got := FormatRedeemed("<X>", "P&Q", " ", " ")
	if !strings.Contains(got, "&lt;X&gt;") || !strings.Contains(got, "P&amp;Q") || !strings.Contains(got, "账号: —") {
		t.Fatal(got)
	}
	if got != FormatRedeemed("<X>", "P&Q", " ", " ") {
		t.Fatal("not pure")
	}
	for _, attempts := range []int{0, 1, 10, 100, 1000000000} {
		if d := retryDelay(attempts); d < 5*time.Second || d > time.Hour {
			t.Fatalf("backoff %d: %v", attempts, d)
		}
	}
}

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/gin-gonic/gin"
)

const precheckTestSession = `{"accessToken":"eyJa.eyJb.c","sessionToken":"st-1"}`

func withPrecheckUpstream(t *testing.T, fn func(context.Context, string) (*cardplatform.DirectPreflightResult, error)) *int32 {
	t.Helper()
	var calls int32
	prev := precheckUpstream
	precheckUpstream = func(ctx context.Context, s string) (*cardplatform.DirectPreflightResult, error) {
		atomic.AddInt32(&calls, 1)
		return fn(ctx, s)
	}
	precheckResults.reset()
	precheckIPMinuteLimiter.reset()
	precheckIPHourLimiter.reset()
	precheckGlobalLimiter.reset()
	t.Cleanup(func() {
		precheckUpstream = prev
		precheckResults.reset()
		precheckIPMinuteLimiter.reset()
		precheckIPHourLimiter.reset()
		precheckGlobalLimiter.reset()
	})
	return &calls
}

func postPrecheck(t *testing.T, session string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/precheck", PublicSessionPrecheck)
	body, _ := json.Marshal(map[string]string{"session": session})
	req := httptest.NewRequest(http.MethodPost, "/precheck", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestPrecheckValidAndCached(t *testing.T) {
	calls := withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) {
		return &cardplatform.DirectPreflightResult{Email: "a@b.com", CurrentPlan: "free"}, nil
	})
	code, out := postPrecheck(t, precheckTestSession)
	if code != 200 || out["valid"] != true || out["email"] != "a@b.com" || out["current_plan"] != "free" {
		t.Fatalf("code=%d out=%v", code, out)
	}
	code, out = postPrecheck(t, precheckTestSession)
	if code != 200 || out["cached"] != true || atomic.LoadInt32(calls) != 1 {
		t.Fatalf("second call should hit cache: code=%d out=%v calls=%d", code, out, *calls)
	}
}

func TestPrecheckRejectsAccessTokenOnlyWithoutUpstream(t *testing.T) {
	calls := withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) {
		t.Fatal("upstream must not be called")
		return nil, nil
	})
	for _, s := range []string{"eyJa.eyJb.c", `{"accessToken":"eyJa.eyJb.c"}`} {
		code, out := postPrecheck(t, s)
		if code != 200 || out["valid"] != false || out["reason_code"] != "SESSION_TOKEN_MISSING" {
			t.Fatalf("%s: code=%d out=%v", s, code, out)
		}
	}
	if *calls != 0 {
		t.Fatalf("calls = %d", *calls)
	}
}

func TestPrecheckInvalidSessionIsVerdictNotError(t *testing.T) {
	withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) {
		return nil, &cardplatform.APIError{HTTPStatus: 400, Code: 400, ErrorCode: "GPT_SESSION_INVALID", Msg: "凭据无效"}
	})
	code, out := postPrecheck(t, precheckTestSession)
	if code != 200 || out["valid"] != false || out["reason_code"] != "GPT_SESSION_INVALID" {
		t.Fatalf("code=%d out=%v", code, out)
	}
}

func TestPrecheckSiteProblemsHideUpstreamDetail(t *testing.T) {
	cases := []error{
		&cardplatform.APIError{HTTPStatus: 403, Msg: "当前 IP 不在白名单内"},
		&cardplatform.APIError{HTTPStatus: 503, ErrorCode: "GPT_DIRECT_PAUSED", Msg: "已暂停"},
		errors.New("dial tcp 1.2.3.4:443: timeout"),
		errPrecheckUnavailable,
	}
	for _, e := range cases {
		withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) { return nil, e })
		code, out := postPrecheck(t, precheckTestSession)
		if code != http.StatusServiceUnavailable || out["error"] != "Session 校验暂不可用，请稍后再试" {
			t.Fatalf("%v: code=%d out=%v", e, code, out)
		}
		// 服务问题不缓存，恢复后能立即重试
		if _, ok := precheckResults.get(precheckKey(`{"accessToken":"eyJa.eyJb.c","sessionToken":"st-1"}`)); ok {
			t.Fatalf("%v: should not cache", e)
		}
	}
}

func TestPrecheckUpstreamRateLimitBecomes429(t *testing.T) {
	withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) {
		return nil, &cardplatform.APIError{HTTPStatus: 429, ErrorCode: "GPT_PREFLIGHT_RATE_LIMITED"}
	})
	code, _ := postPrecheck(t, precheckTestSession)
	if code != http.StatusTooManyRequests {
		t.Fatalf("code = %d", code)
	}
}

func TestPrecheckPerIPLimit(t *testing.T) {
	calls := withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) {
		return nil, &cardplatform.APIError{HTTPStatus: 503, ErrorCode: "GPT_DIRECT_UNAVAILABLE"}
	})
	for i := 0; i < precheckIPMinuteLimiter.limit; i++ {
		if code, _ := postPrecheck(t, precheckTestSession); code == http.StatusTooManyRequests {
			t.Fatalf("limited too early at %d", i)
		}
	}
	if code, _ := postPrecheck(t, precheckTestSession); code != http.StatusTooManyRequests {
		t.Fatalf("code = %d, want 429", code)
	}
	if int(*calls) != precheckIPMinuteLimiter.limit {
		t.Fatalf("calls = %d", *calls)
	}
}

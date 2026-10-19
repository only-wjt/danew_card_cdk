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

func TestPrecheckRejectionHidesUpstreamIdentity(t *testing.T) {
	cases := []struct {
		err  *cardplatform.APIError
		want string
	}{
		{&cardplatform.APIError{HTTPStatus: 400, ErrorCode: "PRECHECK_REJECTED", Msg: "账号已被封禁"}, "账号暂不满足兑换条件：账号已被封禁"},
		{&cardplatform.APIError{HTTPStatus: 400, ErrorCode: "PRECHECK_REJECTED", Msg: "ZovoCard 卡台账户余额不足"}, "账号暂不满足兑换条件"},
		{&cardplatform.APIError{HTTPStatus: 400, ErrorCode: "SOME_NEW_CODE", Msg: "card pool empty"}, "Session 校验未通过"},
		{&cardplatform.APIError{HTTPStatus: 400, ErrorCode: "GPT_X", Msg: "Session 已过期或无权访问，请重新获取后重试，未扣款"}, "Session 校验未通过：Session 已过期或无权访问，请重新获取后重试"},
	}
	for _, tc := range cases {
		withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) { return nil, tc.err })
		code, out := postPrecheck(t, precheckTestSession)
		if code != 200 || out["valid"] != false || out["message"] != tc.want {
			t.Fatalf("%v: code=%d out=%v", tc.err, code, out)
		}
		if rc, _ := out["reason_code"].(string); rc == "SOME_NEW_CODE" {
			t.Fatalf("unknown upstream code leaked: %v", out)
		}
	}
}

func TestPrecheckSiteSideRejectionsAreUnavailable(t *testing.T) {
	for _, ec := range []string{"INSUFFICIENT_BALANCE", "INVALID_REQUEST", "DIRECT_PRODUCT_DISABLED"} {
		withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) {
			return nil, &cardplatform.APIError{HTTPStatus: 400, ErrorCode: ec, Msg: "余额不足"}
		})
		if code, _ := postPrecheck(t, precheckTestSession); code != http.StatusServiceUnavailable {
			t.Fatalf("%s: code = %d", ec, code)
		}
	}
}

func TestPrecheckOneInFlightPerIP(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	calls := withPrecheckUpstream(t, func(context.Context, string) (*cardplatform.DirectPreflightResult, error) {
		entered <- struct{}{}
		<-release
		return &cardplatform.DirectPreflightResult{Email: "onlyWjt.307@gmail.com"}, nil
	})
	done := make(chan int)
	go func() {
		code, _ := postPrecheck(t, precheckTestSession)
		done <- code
	}()
	<-entered
	// 同 IP 第二条（换一条 Session，避免命中缓存）要被挡回，且不计次
	code, out := postPrecheck(t, `{"accessToken":"eyJa.eyJb.c","sessionToken":"st-2"}`)
	if code != http.StatusTooManyRequests || out["error"] != "上一条还在校验中，请等它完成后再校验下一条" {
		t.Fatalf("concurrent: code=%d out=%v", code, out)
	}
	close(release)
	if code := <-done; code != 200 {
		t.Fatalf("first: code=%d", code)
	}
	if *calls != 1 {
		t.Fatalf("calls = %d", *calls)
	}
	// 第一条结束后放行
	go func() { <-entered }()
	if code, _ := postPrecheck(t, `{"accessToken":"eyJa.eyJb.c","sessionToken":"st-3"}`); code != 200 {
		t.Fatalf("after release: code=%d", code)
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

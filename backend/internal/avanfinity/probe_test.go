package avanfinity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeStopsAtBadCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/balance" {
			t.Errorf("should not call %s after balance fails", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key","errorCode":"AUTH_REQUIRED"}`))
	}))
	defer srv.Close()
	res := Probe(context.Background(), &Client{Base: srv.URL, AppID: "app", Secret: "sk", HTTP: srv.Client()})
	if res.OK || len(res.Steps) != 1 || res.Steps[0].State != "fail" || res.Steps[0].Key != "cred" {
		t.Fatalf("probe = %+v", res)
	}
	if !strings.Contains(res.Steps[0].Detail, "401") {
		t.Fatalf("detail = %s", res.Steps[0].Detail)
	}
}

func TestProbeWriteForbiddenIsAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/balance":
			_, _ = w.Write([]byte(`{"code":200,"data":{"balance":"12.00","totalBalance":"12.00"}}`))
		case "/api/v1/x-direct/plans":
			_, _ = w.Write([]byte(`{"code":200,"data":{"plans":[{"key":"premium_3m","enabled":true,"serviceFee":"1.00","pricingVersion":1}],"paymentsEnabled":true}}`))
		case "/api/v1/cards":
			_, _ = w.Write([]byte(`{"code":200,"data":{"total":1,"page":1,"pageSize":100,"list":[{"id":7,"cardNumberMasked":"****4821","status":"active","balance":"3.00"}]}}`))
		case "/api/v1/x-direct/cdks/generate":
			if r.Header.Get("Idempotency-Key") == "" {
				t.Fatal("missing idempotency key")
			}
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"ip"}`))
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	res := Probe(context.Background(), &Client{Base: srv.URL, HTTP: srv.Client()})
	if res.OK || res.Balance != "12.00" {
		t.Fatalf("probe = %+v", res)
	}
	last := res.Steps[len(res.Steps)-1]
	if last.Key != "ip" || last.State != "fail" || !strings.Contains(last.Detail, "白名单") {
		t.Fatalf("last step = %+v", last)
	}
}

func TestProbeWriteConflictMeansAllowlistPassed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/balance":
			_, _ = w.Write([]byte(`{"code":200,"data":{"balance":"5.00","totalBalance":"5.00"}}`))
		case "/api/v1/x-direct/plans":
			_, _ = w.Write([]byte(`{"code":200,"data":{"plans":[],"paymentsEnabled":true}}`))
		case "/api/v1/cards":
			_, _ = w.Write([]byte(`{"code":200,"data":{"list":[{"id":3,"status":"active","balance":"1"}]}}`))
		case "/api/v1/x-direct/cdks/generate":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"单个兑换码的充值本金和全部费用超过钱包授权上限"}`))
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	res := Probe(context.Background(), &Client{Base: srv.URL, HTTP: srv.Client()})
	last := res.Steps[len(res.Steps)-1]
	if !res.OK || last.Key != "ip" || last.State != "ok" || !strings.Contains(last.Detail, "白名单已通过") {
		t.Fatalf("ok=%v last=%+v", res.OK, last)
	}
}

func TestProbeRevokesProbeCode(t *testing.T) {
	var revoked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/balance":
			_, _ = w.Write([]byte(`{"code":200,"data":{"balance":"1.00","totalBalance":"1.00"}}`))
		case r.URL.Path == "/api/v1/x-direct/plans":
			_, _ = w.Write([]byte(`{"code":200,"data":{"plans":[],"paymentsEnabled":false}}`))
		case r.URL.Path == "/api/v1/cards":
			if got := r.URL.Query().Get("page_size"); got != "100" {
				t.Errorf("page_size = %q, want 100", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"list":[{"id":9,"status":"frozen","balance":"0"},{"id":3,"status":"active","balance":"1"}]}}`))
		case r.URL.Path == "/api/v1/x-direct/cdks/generate":
			_, _ = w.Write([]byte(`{"code":200,"data":{"list":[{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","codePrefix":"AVX-7C9E"}],"replayed":false}}`))
		case r.URL.Path == "/api/v1/x-direct/cdks/7c9e6679-7425-40de-944b-e07fc1f90ae7/revoke":
			revoked = true
			_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	res := Probe(context.Background(), &Client{Base: srv.URL, HTTP: srv.Client()})
	if !res.OK || !revoked {
		t.Fatalf("ok=%v revoked=%v steps=%+v", res.OK, revoked, res.Steps)
	}
}

func TestRedeemAlwaysSendsAmountAndLowerCurrency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["expectedAmountMinor"] != float64(800) || body["currency"] != "usd" {
			t.Errorf("body = %+v", body)
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{"plan":"premium_3m","status":"paying"}}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	out, err := c.RedeemCDK(context.Background(), "AVX-1", strings.Repeat("d", 32), "req", "USD", 800)
	if err != nil || out.Status != "paying" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

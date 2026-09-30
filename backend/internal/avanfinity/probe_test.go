package avanfinity

import (
	"context"
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

func TestProbeRevokesProbeCode(t *testing.T) {
	var revoked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/balance":
			_, _ = w.Write([]byte(`{"code":200,"data":{"balance":"1.00","totalBalance":"1.00"}}`))
		case r.URL.Path == "/api/v1/x-direct/plans":
			_, _ = w.Write([]byte(`{"code":200,"data":{"plans":[],"paymentsEnabled":false}}`))
		case r.URL.Path == "/api/v1/cards":
			_, _ = w.Write([]byte(`{"code":200,"data":{"list":[{"id":9,"status":"frozen","balance":"0"},{"id":3,"status":"active","balance":"1"}]}}`))
		case r.URL.Path == "/api/v1/x-direct/cdks/generate":
			_, _ = w.Write([]byte(`{"code":200,"data":{"list":[{"id":42}],"replayed":false}}`))
		case r.URL.Path == "/api/v1/x-direct/cdks/42/revoke":
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

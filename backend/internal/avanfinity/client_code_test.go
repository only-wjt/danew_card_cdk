package avanfinity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicCDKAcceptsWrappedAndFlatBodies(t *testing.T) {
	for _, body := range []string{
		`{"code":0,"data":{"plan":"premium_3m","status":"quoted","amountMinor":1200,"currency":"usd"}}`,
		`{"plan":"premium_3m","status":"quoted","amountMinor":1200,"currency":"usd"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/public/x-cdk/preflight" {
				t.Errorf("path %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(body))
		}))
		pub, err := (&Client{Base: srv.URL, HTTP: srv.Client()}).PreflightCDK(context.Background(), "code", "dev", "name", "req")
		srv.Close()
		if err != nil || pub.Plan != "premium_3m" || pub.Status != "quoted" || pub.AmountMinor != 1200 {
			t.Fatalf("%s: err=%v pub=%+v", body, err, pub)
		}
	}
}

func TestBalanceAcceptsCodeZeroAndRejectsOthers(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"code":0,"data":{"balance":"5.00"}}`, true},
		{`{"code":200,"data":{"balance":"5.00"}}`, true},
		{`{"code":500,"data":null}`, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(tc.body))
		}))
		b, err := (&Client{Base: srv.URL, AppID: "app", Secret: "sk", HTTP: srv.Client()}).GetBalance(context.Background())
		srv.Close()
		if tc.ok && (err != nil || b.Balance != "5.00") {
			t.Fatalf("%s: err=%v b=%+v", tc.body, err, b)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%s: want error", tc.body)
		}
	}
}

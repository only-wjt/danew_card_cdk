package avanfinity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

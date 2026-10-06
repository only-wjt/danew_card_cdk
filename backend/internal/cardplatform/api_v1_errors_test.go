package cardplatform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIv1ErrorFieldSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"API 密钥无效"}`))
	}))
	defer srv.Close()
	c := New(Config{SiteBase: srv.URL, APIKey: "s", AppID: "a", APIv1: true})
	_, err := c.GetBalance(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 401 || apiErr.Msg != "API 密钥无效" {
		t.Fatalf("err = %v", err)
	}
}

func TestAPIv1RequiresAppID(t *testing.T) {
	c := New(Config{SiteBase: "http://127.0.0.1:1", APIKey: "s", APIv1: true})
	_, err := c.GetBalance(context.Background())
	if err == nil || !strings.Contains(err.Error(), "App ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestGetBalanceCamelCase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-App-Id") != "a" || r.Header.Get("X-App-Secret") != "s" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"spendableBalance":"12.50","totalBalance":"20.00","accountReserveAmount":7.5}}`))
	}))
	defer srv.Close()
	c := New(Config{SiteBase: srv.URL, APIKey: "s", AppID: "a", APIv1: true})
	bal, err := c.GetBalance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bal.SpendableBalance != "12.50" || bal.Balance != "20.00" || bal.AccountReserveAmount != "7.5" {
		t.Fatalf("bal = %+v", bal)
	}
}

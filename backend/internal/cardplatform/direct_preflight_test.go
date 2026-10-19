package cardplatform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirectPreflightSendsOnlyCredential(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v1/gpt-direct/preflight" || r.Header.Get("X-API-Key") != "k" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"email":"a@b.com","currentPlan":"free","preflight_token":"pt","quotes":{}}}`))
	}))
	defer srv.Close()

	res, err := New(Config{SiteBase: srv.URL, APIKey: "k"}).DirectPreflight(context.Background(), `{"sessionToken":"st"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Email != "a@b.com" || res.CurrentPlan != "free" {
		t.Fatalf("res = %+v", res)
	}
	if len(got) != 1 {
		t.Fatalf("body should carry only credential, got %v", got)
	}
	cred, _ := got["credential"].(map[string]any)
	if cred["mode"] != "session" || cred["session"] != `{"sessionToken":"st"}` {
		t.Fatalf("credential = %v", cred)
	}
}

func TestDirectPreflightSurfacesErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"msg":"凭据无效","error_code":"GPT_SESSION_INVALID"}`))
	}))
	defer srv.Close()

	_, err := New(Config{SiteBase: srv.URL, APIKey: "k"}).DirectPreflight(context.Background(), "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorCode != "GPT_SESSION_INVALID" || apiErr.HTTPStatus != 400 {
		t.Fatalf("err = %v", err)
	}
}

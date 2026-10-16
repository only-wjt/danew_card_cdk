package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 路由注册不能 panic：同一层级混用 :id 和静态段会让 gin 直接崩在启动阶段，
// 而这类错误只有真的把 engine 建起来才暴露。
func TestSetupRoutesDoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupRoutes(r)

	want := map[string]string{
		"GET /api/v1/admin/card-platforms":                "",
		"POST /api/v1/admin/card-platforms/upsert":        "",
		"PUT /api/v1/admin/card-platforms/dual-bind":      "",
		"GET /api/v1/admin/card-platforms/bindings":       "",
		"POST /api/v1/admin/card-platforms/status":        "",
		"POST /api/v1/admin/card-platforms/reset-circuit": "",
		"POST /api/v1/admin/card-platforms/ping":          "",
		"POST /api/v1/admin/card-platforms/webhook-url":   "",
		"POST /api/v1/webhooks/epay":                      "",
		"POST /api/v1/webhooks/cardplatform":              "",
		"POST /api/v1/webhooks/cardplatform/:accountId":   "",
		"POST /api/v1/webhooks/avanfinity":                "",
		"GET /api/v1/lookup/cdk":                          "",
		"POST /api/v1/lookup/cdk/batch":                   "",
	}
	for _, ri := range r.Routes() {
		key := ri.Method + " " + ri.Path
		delete(want, key)
		if strings.Contains(ri.Path, "/webhooks/*") {
			t.Errorf("catch-all webhook route conflicts with epay: %s", key)
		}
	}
	for missing := range want {
		t.Errorf("route not registered: %s", missing)
	}
}

// Caddy 在橙云下把真实访客写进 X-Real-IP，并在 X-Forwarded-For 末尾追加 CF 边缘 IP。
// 只有来自可信代理的请求才采信头；直连伪造的头必须被忽略。
func TestClientIPBehindCaddy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies([]string{"172.16.0.0/12"}); err != nil {
		t.Fatal(err)
	}
	r.RemoteIPHeaders = []string{"X-Real-IP", "X-Forwarded-For"}
	var got string
	r.GET("/ip", func(c *gin.Context) { got = c.ClientIP() })

	do := func(remote string, hdr map[string]string) string {
		req := httptest.NewRequest("GET", "/ip", nil)
		req.RemoteAddr = remote
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		r.ServeHTTP(httptest.NewRecorder(), req)
		return got
	}

	if ip := do("172.18.0.3:5555", map[string]string{
		"X-Real-IP":       "203.0.113.7",
		"X-Forwarded-For": "203.0.113.7, 162.158.1.1",
	}); ip != "203.0.113.7" {
		t.Fatalf("via caddy = %s", ip)
	}
	if ip := do("198.51.100.9:5555", map[string]string{"X-Real-IP": "1.2.3.4"}); ip != "198.51.100.9" {
		t.Fatalf("spoofed header from untrusted peer honoured: %s", ip)
	}
}

func TestPlaceholderSecretRejected(t *testing.T) {
	for _, s := range []string{"change-me-to-random-32chars-min", "your-secret-key-change-in-production", "my-EXAMPLE-secret-123456"} {
		if !isPlaceholderSecret(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
	if isPlaceholderSecret("9f2c4b7e1a0d3c5e8b6f4a2d1c0e9b8a") {
		t.Error("random secret rejected")
	}
}

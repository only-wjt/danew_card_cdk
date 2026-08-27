package server

import (
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
		"GET /api/v1/admin/card-platforms":                 "",
		"POST /api/v1/admin/card-platforms/upsert":         "",
		"PUT /api/v1/admin/card-platforms/dual-bind":       "",
		"GET /api/v1/admin/card-platforms/bindings":        "",
		"POST /api/v1/admin/card-platforms/status":         "",
		"POST /api/v1/admin/card-platforms/reset-circuit":  "",
		"POST /api/v1/admin/card-platforms/ping":           "",
		"POST /api/v1/admin/card-platforms/webhook-url":    "",
		"POST /api/v1/webhooks/epay":                       "",
		"POST /api/v1/webhooks/cardplatform":              "",
		"POST /api/v1/webhooks/cardplatform/:accountId":    "",
		"POST /api/v1/webhooks/avanfinity":                "",
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

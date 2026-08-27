package handler

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestWebhookSignatureMatchesBodyOnly(t *testing.T) {
	secret := "whsec_abc"
	raw := []byte(`{"event":"webhook.test"}`)
	got := hmacHex(secret, raw)
	if !webhookSignatureMatches(secret, raw, nil, []string{got}) {
		t.Fatal("body-only signature should match")
	}
	if webhookSignatureMatches(secret, raw, nil, []string{"deadbeef"}) {
		t.Fatal("wrong signature must not match")
	}
}

func TestWebhookSignatureMatchesTimestampAndPrefix(t *testing.T) {
	secret := "whsec_abc"
	raw := []byte(`{"event":"webhook.test"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := append(append([]byte(ts), '.'), raw...)
	got := "v1=" + hmacHex(secret, msg)
	if !webhookSignatureMatches(secret, raw, []string{ts}, []string{got}) {
		t.Fatal("timestamped v1= signature should match")
	}
}

func TestWebhookSignatureCandidatesReadAvanfinityHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/cardplatform", nil)
	req.Header.Set("X-Avanfinity-Signature", "v1=abcd")
	c.Request = req
	gots := webhookSignatureCandidates(c)
	if len(gots) == 0 {
		t.Fatal("expected avanfinity signature header")
	}
}

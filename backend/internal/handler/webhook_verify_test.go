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

func TestWebhookSignatureMatchesOfficialAvanfinityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "whsec_official"
	raw := []byte(`{"event":"webhook.test"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := append(append([]byte(ts), '.'), raw...)
	sig := "v1=" + hmacHex(secret, msg)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/cardplatform/2", nil)
	req.Header.Set("X-Avanfinity-Webhook-Id", "evt_test_1")
	req.Header.Set("X-Avanfinity-Webhook-Timestamp", ts)
	req.Header.Set("X-Avanfinity-Signature", sig)
	c.Request = req

	gots := webhookSignatureCandidates(c)
	timestamps := webhookTimestamps(c)
	if !webhookSignatureMatches(secret, raw, timestamps, gots) {
		t.Fatalf("official avanfinity headers should verify, present=%v ts=%v", presentWebhookHeaderNames(c), timestamps)
	}
	if !webhookTimestampSkewOK(ts, time.Now()) {
		t.Fatal("fresh timestamp should be within skew")
	}
}

func TestWebhookTimestampSkewAcceptsMilliseconds(t *testing.T) {
	now := time.Now()
	ms := strconv.FormatInt(now.UnixMilli(), 10)
	if !webhookTimestampSkewOK(ms, now) {
		t.Fatal("millisecond timestamp within 300s should pass")
	}
	old := strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10)
	if webhookTimestampSkewOK(old, now) {
		t.Fatal("10-minute-old timestamp should fail skew")
	}
}

func TestWebhookTimestampsReadAvanfinityWebhookTimestamp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/cardplatform/2", nil)
	req.Header.Set("X-Avanfinity-Webhook-Timestamp", "1710000000")
	c.Request = req
	got := webhookTimestamps(c)
	if len(got) != 1 || got[0] != "1710000000" {
		t.Fatalf("timestamps = %#v", got)
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

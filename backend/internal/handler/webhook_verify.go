package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const webhookTimestampSkewSec = 300

func webhookSignatureCandidates(c *gin.Context) []string {
	names := []string{
		"X-Avanfinity-Signature",
		"X-Signature",
		"X-Webhook-Signature",
	}
	out := make([]string, 0, 8)
	seen := map[string]bool{}
	var add func(string)
	add = func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		key := strings.ToLower(v)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, v)
		for _, prefix := range []string{"v1=", "sha256="} {
			if strings.HasPrefix(key, prefix) {
				add(v[len(prefix):])
			}
		}
	}
	for _, name := range names {
		add(c.GetHeader(name))
	}
	return out
}

func webhookTimestampHeaderNames() []string {
	return []string{
		"X-Avanfinity-Webhook-Timestamp",
		"X-Webhook-Timestamp",
		"X-Avanfinity-Timestamp",
		"X-Timestamp",
	}
}

func webhookTimestamps(c *gin.Context) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, name := range webhookTimestampHeaderNames() {
		ts := strings.TrimSpace(c.GetHeader(name))
		if ts == "" || seen[ts] {
			continue
		}
		seen[ts] = true
		out = append(out, ts)
	}
	return out
}

func webhookTimestampSkewOK(ts string, now time.Time) bool {
	unix, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
	if err != nil {
		return true
	}
	if unix > 1_000_000_000_000 {
		unix /= 1000
	}
	delta := now.Unix() - unix
	if delta < 0 {
		delta = -delta
	}
	return delta <= webhookTimestampSkewSec
}

func presentWebhookHeaderNames(c *gin.Context) []string {
	names := []string{
		"X-Avanfinity-Signature",
		"X-Avanfinity-Webhook-Timestamp",
		"X-Avanfinity-Webhook-Id",
		"X-Signature",
		"X-Webhook-Signature",
		"X-Webhook-Timestamp",
		"X-Avanfinity-Timestamp",
		"X-Timestamp",
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if strings.TrimSpace(c.GetHeader(name)) != "" {
			out = append(out, name)
		}
	}
	return out
}

func hmacHex(secret string, msg []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(msg)
	return hex.EncodeToString(mac.Sum(nil))
}

func webhookSecretVariants(secret string) []string {
	secret = strings.TrimSpace(secret)
	out := []string{secret}
	if trimmed := strings.TrimPrefix(secret, "whsec_"); trimmed != secret && trimmed != "" {
		out = append(out, trimmed)
	}
	return out
}

func webhookSignedMessages(raw []byte, timestamps []string) [][]byte {
	msgs := make([][]byte, 0, 1+len(timestamps))
	if len(timestamps) == 0 {
		msgs = append(msgs, raw)
	}
	for _, ts := range timestamps {
		buf := make([]byte, 0, len(ts)+1+len(raw))
		buf = append(buf, ts...)
		buf = append(buf, '.')
		buf = append(buf, raw...)
		msgs = append(msgs, buf)
	}
	if len(timestamps) > 0 {
		msgs = append(msgs, raw)
	}
	return msgs
}

func webhookSignatureMatches(secret string, raw []byte, timestamps, gots []string) bool {
	if strings.TrimSpace(secret) == "" || len(gots) == 0 {
		return false
	}
	expects := make([]string, 0, 8)
	for _, sec := range webhookSecretVariants(secret) {
		for _, msg := range webhookSignedMessages(raw, timestamps) {
			expects = append(expects, hmacHex(sec, msg))
		}
	}
	for _, got := range gots {
		got = strings.ToLower(strings.TrimSpace(got))
		for _, prefix := range []string{"v1=", "sha256="} {
			got = strings.TrimPrefix(got, prefix)
		}
		for _, expect := range expects {
			if subtle.ConstantTimeCompare([]byte(got), []byte(strings.ToLower(expect))) == 1 {
				return true
			}
		}
	}
	return false
}

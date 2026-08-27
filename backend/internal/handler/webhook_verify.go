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

func webhookSignatureCandidates(c *gin.Context) []string {
	names := []string{"X-Signature", "X-Avanfinity-Signature", "X-Webhook-Signature"}
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

func webhookTimestamps(c *gin.Context) []string {
	names := []string{"X-Webhook-Timestamp", "X-Avanfinity-Timestamp", "X-Timestamp"}
	out := make([]string, 0, 3)
	seen := map[string]bool{}
	now := time.Now().Unix()
	for _, name := range names {
		ts := strings.TrimSpace(c.GetHeader(name))
		if ts == "" || seen[ts] {
			continue
		}
		seen[ts] = true
		if unix, err := strconv.ParseInt(ts, 10, 64); err == nil {
			if delta := now - unix; delta > 300 || delta < -300 {
				continue
			}
		}
		out = append(out, ts)
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
	msgs := [][]byte{raw}
	for _, ts := range timestamps {
		buf := make([]byte, 0, len(ts)+1+len(raw))
		buf = append(buf, ts...)
		buf = append(buf, '.')
		buf = append(buf, raw...)
		msgs = append(msgs, buf)
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

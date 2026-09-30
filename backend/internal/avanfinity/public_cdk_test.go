package avanfinity

import (
	"encoding/json"
	"testing"
)

func TestPublicCDKHasNoMoneyFlags(t *testing.T) {
	raw := []byte(`{
		"plan":"premium_3m",
		"status":"ineligible",
		"recipient":"alice",
		"canRetryPreflight":false,
		"fundingDispatched":true,
		"paymentDispatched":true,
		"paymentAttempted":true,
		"estimatedUsd":"9.00"
	}`)
	var pub PublicCDK
	if err := json.Unmarshal(raw, &pub); err != nil {
		t.Fatal(err)
	}
	if pub.Status != "ineligible" || pub.Recipient != "alice" {
		t.Fatalf("%+v", pub)
	}
	if pub.CanRetryPreflight == nil || *pub.CanRetryPreflight {
		t.Fatal("false must stay false")
	}
	// 发行者字段即使出现在 JSON 里，也不能进公开结构，避免被当成「没动钱」。
	again, _ := json.Marshal(pub)
	var echo map[string]any
	if err := json.Unmarshal(again, &echo); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"fundingDispatched", "paymentDispatched", "paymentAttempted", "estimatedUsd"} {
		if _, ok := echo[key]; ok {
			t.Fatalf("public snapshot leaked %s", key)
		}
	}
}

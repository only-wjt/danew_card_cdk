package handler

import "testing"

// 没有本站选卡配置时，地区也必须带进发码偏好，否则会静默退回菲律宾。
func TestBatchIssuePrefsCarriesRegion(t *testing.T) {
	openHandlerTestDB(t)
	prefs := batchIssuePrefs("CL")
	if len(prefs) != 1 || prefs[0].PaymentCountry != "CL" {
		t.Fatalf("prefs = %+v", prefs)
	}
}

func TestBatchFingerprintIncludesRegion(t *testing.T) {
	items := []batchRechargeCredential{{Mode: "session", Session: "s1"}}
	ph := batchFingerprint("op", "plus", "", items)
	cl := batchFingerprint("op", "plus", "CL", items)
	if ph == cl {
		t.Fatal("different regions must not dedupe into one batch")
	}
	if ph != batchFingerprint("op", "plus", "", items) {
		t.Fatal("fingerprint not stable")
	}
}

func TestBatchIssuePrefsDefaultRegion(t *testing.T) {
	openHandlerTestDB(t)
	// 默认地区：只带本站选卡偏好（与单张发码一致），不带国家码
	for _, p := range batchIssuePrefs("") {
		if p.PaymentCountry != "" {
			t.Fatalf("default region leaked country: %+v", p)
		}
	}
}

package db

import (
	"encoding/json"
	"testing"
)

func TestCDKRegionMigrationAndCache(t *testing.T) {
	openTestDB(t)
	if err := SaveCardplatformCDKCode(1, "ZC-SYNTHETIC-OLD-CODE", "ZC-SYNTHETIC", "plus", 15); err != nil {
		t.Fatal(err)
	}
	list, _, err := ListCardplatformStoredCDKCodesPage("", "", "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].PaymentCountry != nil || list[0].FeeAmountMinor != 15 {
		t.Fatalf("legacy row changed: %+v", list)
	}
	raw, err := json.Marshal(list[0])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["payment_country"] != nil {
		t.Fatal("unknown region became default")
	}
	if err := UpdateCardplatformCDKRegion(1, " cl "); err != nil {
		t.Fatal(err)
	}
	if err := SaveCardplatformCDKCode(2, "ZC-SYNTHETIC-JP-CODE", "", "plus", 15, "JP"); err != nil {
		t.Fatal(err)
	}
	if err := SaveCardplatformCDKCode(3, "ZC-SYNTHETIC-DEFAULT", "", "plus", 15, ""); err != nil {
		t.Fatal(err)
	}
	// 旧缓存回填不带地区，不能擦掉已同步的地区。
	if err := SaveCardplatformCDKCode(2, "ZC-SYNTHETIC-JP-CODE", "", "plus", 15); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCardplatformCDKStatus(1, "disabled"); err != nil {
		t.Fatal(err)
	}
	list, n, err := ListCardplatformStoredCDKCodesPage("", "", "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if n < 3 {
		t.Fatal(n)
	}
	for _, r := range list {
		want := map[int64]string{1: "CL", 2: "JP", 3: ""}[r.UpstreamID]
		if _, ok := map[int64]string{1: "CL", 2: "JP", 3: ""}[r.UpstreamID]; !ok {
			continue
		}
		if r.PaymentCountry == nil || *r.PaymentCountry != want {
			t.Fatalf("wrong cached region id=%d got=%v", r.UpstreamID, r.PaymentCountry)
		}
		if r.UpstreamID == 1 && r.Status != "disabled" {
			t.Fatal("status lost")
		}
	}
}

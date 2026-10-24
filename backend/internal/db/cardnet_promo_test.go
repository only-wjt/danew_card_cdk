package db

import (
	"fmt"
	"testing"
)

// 卡网 Grok 首月半价码：码上 promo_only 标记的写入、存量回填、列表筛选与统计分行。
func TestCardnetPromoFlag(t *testing.T) {
	openTestDB(t)
	client, _, err := CreateCardnetClient(CardnetClientInput{
		Name: "dujiao", AllowedPlans: []string{"grok_premium_monthly", "grok_premium_monthly_promo"}, DailyCodeLimit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	issue := func(key, plan string, promo bool, codes ...string) {
		t.Helper()
		req, _, err := CreateOrGetCardnetRequest(CardnetIssueParams{
			ClientID: client.ID, IdempotencyKey: key, Plan: plan, PaymentCountry: "US", Count: len(codes), AccountID: 1, DailyCodeLimit: 50,
		})
		if err != nil {
			t.Fatal(err)
		}
		save := make([]CardnetCodeToSave, 0, len(codes))
		for i, c := range codes {
			save = append(save, CardnetCodeToSave{UpstreamID: int64(9000 + len(key)*10 + i), Code: c, Plan: "grok_premium_monthly", FeeAmountMinor: 100, PromoOnly: promo})
		}
		if err := CompleteCardnetRequest(req, save); err != nil {
			t.Fatal(err)
		}
	}
	issue("k-promo", "grok_premium_monthly_promo", true, "GRK-PROMO-0001", "GRK-PROMO-0002")
	issue("k-full", "grok_premium_monthly", false, "GRK-FULL-0001")
	// 加列之前发出的半价码：落库时没有标记，迁移按请求档位补上。
	issue("k-old", "grok_premium_monthly_promo", false, "GRK-OLD-0001")
	if err := migrateCardnet(); err != nil {
		t.Fatal(err)
	}

	for code, want := range map[string]bool{"GRK-PROMO-0001": true, "grk-old-0001": true, "GRK-FULL-0001": false, "NOT-HERE": false} {
		plan, promo, err := PromoCodePlan(code)
		if err != nil || promo != want || (want && plan != "grok_premium_monthly") {
			t.Errorf("%s: plan=%q promo=%v err=%v", code, plan, promo, err)
		}
	}

	for filter, want := range map[string]int{"yes": 3, "no": 1, "": 4} {
		list, total, err := ListStoredCDKsDetailed(StoredCDKListQuery{Promo: filter, PageSize: 50})
		if err != nil || total != want || len(list) != want {
			t.Fatalf("promo=%q: total=%d len=%d err=%v", filter, total, len(list), err)
		}
		for _, it := range list {
			if it.PromoOnly != (it.Code != "GRK-FULL-0001") {
				t.Fatalf("promo_only for %s: %v", it.Code, it.PromoOnly)
			}
		}
	}

	stats, err := CardnetStats(client.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, r := range stats {
		got[r.Plan] += r.Codes
	}
	if fmt.Sprint(got) != "map[grok_premium_monthly:1 grok_premium_monthly_promo:3]" {
		t.Fatalf("stats should split promo codes: %v", got)
	}
}

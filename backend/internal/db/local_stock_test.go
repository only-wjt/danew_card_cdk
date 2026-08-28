package db

import "testing"

func TestMigrateSeedsGPTWhiteDefaultPrice(t *testing.T) {
	openTestDB(t)
	got, err := GetAgentDefaultPlanPrices()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got[PlanGPTWhite] != PlanGPTWhiteDefaultCents {
		t.Fatalf("seeded gpt_white = %#v", got)
	}
}

func TestParseLocalStockImportLines(t *testing.T) {
	got := ParseLocalStockImportLines("a@x.com\r\nb@y.com:pw\n\n  \n c@z.com ")
	if len(got) != 3 || got[0] != "a@x.com" || got[1] != "b@y.com:pw" || got[2] != "c@z.com" {
		t.Fatalf("parsed = %#v", got)
	}
}

func TestSeedLocalStockDefaultPricesMerges(t *testing.T) {
	openTestDB(t)
	if err := SetAgentDefaultPlanPrices(AgentPlanPriceMap{"plus": 1500}); err != nil {
		t.Fatalf("set plus: %v", err)
	}
	if err := seedLocalStockDefaultPrices(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := GetAgentDefaultPlanPrices()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got["plus"] != 1500 {
		t.Fatalf("wiped plus: %#v", got)
	}
	if got[PlanGPTWhite] != PlanGPTWhiteDefaultCents {
		t.Fatalf("gpt_white = %d want %d", got[PlanGPTWhite], PlanGPTWhiteDefaultCents)
	}
	if err := seedLocalStockDefaultPrices(); err != nil {
		t.Fatalf("seed again: %v", err)
	}
	got, _ = GetAgentDefaultPlanPrices()
	if got[PlanGPTWhite] != PlanGPTWhiteDefaultCents {
		t.Fatalf("second seed changed price: %#v", got)
	}
}

func TestImportLocalStockUniqueUpstreamIDs(t *testing.T) {
	openTestDB(t)
	n, skipped, err := ImportLocalStockCodes(PlanGPTWhite, []string{
		"one@example.com",
		"two@example.com:secret",
		"one@example.com",
		"",
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if n != 2 {
		t.Fatalf("imported=%d want 2 skipped=%v", n, skipped)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped=%v want 1 existing", skipped)
	}
	row1, ok := LookupStoredCDKDetail("one@example.com")
	if !ok || row1.UpstreamID >= 0 || row1.Plan != PlanGPTWhite {
		t.Fatalf("row1=%+v ok=%v", row1, ok)
	}
	row2, ok := LookupStoredCDKDetail("two@example.com:secret")
	if !ok || row2.UpstreamID >= 0 {
		t.Fatalf("row2=%+v ok=%v", row2, ok)
	}
	if row1.UpstreamID == row2.UpstreamID {
		t.Fatalf("shared upstream_id %d", row1.UpstreamID)
	}
	if err := ConsumeAgentCDK(row1.Code); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if _, _, _, st, ok := LookupStoredCDKByCode(row1.Code); !ok || st != "consumed" {
		t.Fatalf("consumed status=%q", st)
	}
	if _, _, _, st, ok := LookupStoredCDKByCode(row2.Code); !ok || st != "unused" {
		t.Fatalf("sibling should stay unused, got %q", st)
	}
}

func TestClaimUnassignedLocalStock(t *testing.T) {
	openTestDB(t)
	agent, err := CreateAgentUser("whiteagent", "AgentTestPass2026", "W", nil)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if _, _, err := ImportLocalStockCodes(PlanGPTWhite, []string{"a@x.com", "b@x.com"}); err != nil {
		t.Fatalf("import: %v", err)
	}
	codes, err := ClaimUnassignedLocalStock(agent.ID, PlanGPTWhite, 2)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(codes) != 2 {
		t.Fatalf("codes=%v", codes)
	}
	for _, code := range codes {
		row, ok := LookupStoredCDKDetail(code)
		if !ok || row.AssignedAgentUserID != agent.ID {
			t.Fatalf("not assigned: %+v ok=%v", row, ok)
		}
	}
	if _, err := ClaimUnassignedLocalStock(agent.ID, PlanGPTWhite, 1); err == nil {
		t.Fatal("expected stock shortage")
	}
	sum, err := ListLocalStockSummaries()
	if err != nil || len(sum) != 1 || sum[0].Unassigned != 0 || sum[0].Assigned != 2 {
		t.Fatalf("summary=%+v err=%v", sum, err)
	}
}

// 档位 key 大小写不一致时，绝不能出现「下单过了、扣款了、发货时查不到库存」。
func TestLocalStockPlanKeyIsCaseInsensitive(t *testing.T) {
	openTestDB(t)
	agent, err := CreateAgentUser("caseagent", "AgentTestPass2026", "C", nil)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if _, _, err := ImportLocalStockCodes("GPT_White", []string{"case@x.com"}); err != nil {
		t.Fatalf("import: %v", err)
	}
	n, err := CountUnassignedLocalStock("GPT_WHITE")
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	codes, err := ClaimUnassignedLocalStock(agent.ID, "Gpt_White", 1)
	if err != nil || len(codes) != 1 {
		t.Fatalf("claim codes=%v err=%v", codes, err)
	}
	if n, err := CountUnassignedLocalStock(PlanGPTWhite); err != nil || n != 0 {
		t.Fatalf("after claim count=%d err=%v", n, err)
	}
}

func TestCanonicalPlanKeyFoldsPro(t *testing.T) {
	if CanonicalPlanKey("pro") != "pro_20x" {
		t.Fatal(CanonicalPlanKey("pro"))
	}
	if CanonicalPlanKey("GPTPRO") != "pro_20x" {
		t.Fatal(CanonicalPlanKey("GPTPRO"))
	}
	if CanonicalPlanKey("pro_20x") != "pro_20x" {
		t.Fatal(CanonicalPlanKey("pro_20x"))
	}
	if CanonicalPlanKey("plus") != "plus" {
		t.Fatal(CanonicalPlanKey("plus"))
	}
}

func TestCheckAgentCDKForRechargeBlocksGPTWhite(t *testing.T) {
	openTestDB(t)
	agent, err := CreateAgentUser("rechargeblock", "AgentTestPass2026", "R", nil)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if _, _, err := ImportLocalStockCodes(PlanGPTWhite, []string{"white@example.com"}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := ClaimUnassignedLocalStock(agent.ID, PlanGPTWhite, 1); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := CheckAgentCDKForRecharge(agent.ID, PlanGPTWhite, "white@example.com"); err != ErrCDKLocalStock {
		t.Fatalf("want ErrCDKLocalStock got %v", err)
	}
}

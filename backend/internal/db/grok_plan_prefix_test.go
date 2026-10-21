package db

import "testing"

// 卡台同步回来的是裸档位：已存的 grok_ 前缀不能被冲掉，否则代理补发会按 X 档重新发码。
func TestSyncKeepsGrokPlanPrefix(t *testing.T) {
	openTestDB(t)
	if err := SaveCardplatformCDKCode(9, "ZC-GROK-PREFIX-TEST-0001", "ZC-GROK-PREFIX", "grok_premium_monthly", 100, "US"); err != nil {
		t.Fatal(err)
	}
	if err := SaveCardplatformCDKCodeWithStatus(9, "ZC-GROK-PREFIX-TEST-0001", "", "premium_monthly", 0, "unused"); err != nil {
		t.Fatal(err)
	}
	_, plan, _, _, ok := LookupStoredCDKByCode("ZC-GROK-PREFIX-TEST-0001")
	if !ok || plan != "grok_premium_monthly" {
		t.Fatalf("grok prefix lost: %q", plan)
	}
	// 真正换了档位时照常更新
	if err := SaveCardplatformCDKCodeWithStatus(9, "ZC-GROK-PREFIX-TEST-0001", "", "plus", 0, "unused"); err != nil {
		t.Fatal(err)
	}
	if _, plan, _, _, _ = LookupStoredCDKByCode("ZC-GROK-PREFIX-TEST-0001"); plan != "plus" {
		t.Fatalf("plan not updated: %q", plan)
	}
}

// 不同卡台账户的 upstream_id 可以重复，地区必须按完整码读写。
func TestCDKRegionByCodeIgnoresUpstreamIDCollision(t *testing.T) {
	openTestDB(t)
	if err := SaveCardplatformCDKCodeForAccount(123, "ZC-ACCOUNT-A-CODE-0001", "", "plus", 0, "unused", 1, "CL"); err != nil {
		t.Fatal(err)
	}
	if err := SaveCardplatformCDKCodeForAccount(123, "ZC-ACCOUNT-B-CODE-0001", "", "plus", 0, "unused", 2, ""); err != nil {
		t.Fatal(err)
	}
	if c, known := LookupStoredCDKRegionByCode("zc-account-a-code-0001"); !known || c != "CL" {
		t.Fatalf("account A region = %q known=%v", c, known)
	}
	if err := UpdateCardplatformCDKRegionByCode("ZC-ACCOUNT-B-CODE-0001", "jp"); err != nil {
		t.Fatal(err)
	}
	if c, _ := LookupStoredCDKRegionByCode("ZC-ACCOUNT-A-CODE-0001"); c != "CL" {
		t.Fatalf("updating B touched A: %q", c)
	}
	if c, _ := LookupStoredCDKRegionByCode("ZC-ACCOUNT-B-CODE-0001"); c != "JP" {
		t.Fatalf("account B region = %q", c)
	}
}

// 启动迁移把之前以裸键落库、可确定是 Grok 的档位补成 grok_ 前缀；裸 premium_* 和 GPT 档不动。
func TestMigrationBackfillsUnambiguousGrokPlans(t *testing.T) {
	openTestDB(t)
	for code, plan := range map[string]string{"ZC-MIG-0001": "super_monthly", "ZC-MIG-0002": "heavy_yearly",
		"ZC-MIG-0003": "premium_monthly", "ZC-MIG-0004": "plus", "ZC-MIG-0005": "monthly"} {
		if _, err := DB.Exec(`INSERT INTO cardplatform_cdk_codes (upstream_id, code, plan, status) VALUES (0, ?, ?, 'unused')`, code, plan); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateCodeLookupIndexes(); err != nil {
		t.Fatal(err)
	}
	if err := migrateCodeLookupIndexes(); err != nil { // 幂等
		t.Fatal(err)
	}
	want := map[string]string{"ZC-MIG-0001": "grok_super_monthly", "ZC-MIG-0002": "grok_ultra_yearly",
		"ZC-MIG-0003": "premium_monthly", "ZC-MIG-0004": "plus", "ZC-MIG-0005": "grok_premium_monthly"}
	for code, plan := range want {
		var got string
		if err := DB.QueryRow(`SELECT plan FROM cardplatform_cdk_codes WHERE code = ?`, code).Scan(&got); err != nil || got != plan {
			t.Fatalf("%s plan=%q err=%v want %q", code, got, err, plan)
		}
	}
}

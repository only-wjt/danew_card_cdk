package db

import (
	"fmt"
	"testing"
)

func TestMigrateSiteDualBindSchema(t *testing.T) {
	openTestDB(t)
	var hasID int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cardplatform_cdk_codes') WHERE name='id'`).Scan(&hasID); err != nil {
		t.Fatal(err)
	}
	if hasID == 0 {
		t.Fatal("expected id column after migrate")
	}
	var acctN int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM card_platform_accounts`).Scan(&acctN); err != nil {
		t.Fatal(err)
	}
	// 无 settings 时可能 0；有 settings 时至少 1
	var bindingTable int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='site_cdk_bindings'`).Scan(&bindingTable); err != nil || bindingTable != 1 {
		t.Fatalf("site_cdk_bindings missing: %v n=%d", err, bindingTable)
	}
}

func TestCreatePendingSiteCDKAndBindings(t *testing.T) {
	openTestDB(t)
	row, err := CreatePendingSiteCDK("DN-TEST1111-22223333-44445555", "plus", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if row.ID <= 0 || row.IssueStatus != IssueStatusPending {
		t.Fatalf("row=%+v", row)
	}
	bid, err := InsertSiteCDKBinding(SiteCDKBinding{
		SiteCodeID: row.ID, SiteCode: row.Code, AccountID: 1, Provider: AccountProtocolSpaceXLegacy,
		RemoteID: "9001", RemoteCode: "GPTD-FAKE-00000001", RemoteCodePrefix: "GPTD-FAKE", IsPrimary: true,
		Status: BindingStatusUnused,
	})
	if err != nil || bid <= 0 {
		t.Fatalf("binding: id=%d err=%v", bid, err)
	}
	if err := ActivateSiteCDK(row.ID, 200); err != nil {
		t.Fatal(err)
	}
	got, ok := GetSiteCDKByCode(row.Code)
	if !ok || got.IssueStatus != IssueStatusActive {
		t.Fatalf("activated=%+v ok=%v", got, ok)
	}
	list, err := ListBindingsForSiteCode(row.Code)
	if err != nil || len(list) != 1 {
		t.Fatalf("bindings=%v err=%v", list, err)
	}
}

func TestClaimBindingForRedeemUsesLease(t *testing.T) {
	openTestDB(t)
	row, err := CreatePendingSiteCDK("DN-LEASE111-22223333-44445555", "plus", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	bindingID, err := InsertSiteCDKBinding(SiteCDKBinding{
		SiteCodeID: row.ID, SiteCode: row.Code, AccountID: 1,
		Provider: AccountProtocolSpaceXLegacy, RemoteID: "lease-1",
		RemoteCode: "GPTD-LEASE-1", IsPrimary: true, Status: BindingStatusUnused,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := ClaimBindingForRedeem(bindingID); err != nil || !ok {
		t.Fatalf("first claim ok=%v err=%v", ok, err)
	}
	if ok, err := ClaimBindingForRedeem(bindingID); err != nil || ok {
		t.Fatalf("concurrent claim must fail: ok=%v err=%v", ok, err)
	}
	if _, err := DB.Exec(`
		UPDATE site_cdk_bindings SET updated_at=datetime('now', '-61 seconds') WHERE id=?
	`, bindingID); err != nil {
		t.Fatal(err)
	}
	if ok, err := ClaimBindingForRedeem(bindingID); err != nil || !ok {
		t.Fatalf("expired lease should retry: ok=%v err=%v", ok, err)
	}
}

func TestLegacyRowCodeKindAfterMigrate(t *testing.T) {
	openTestDB(t)
	if err := SaveCardplatformCDKCode(8001, "GPTD-LEGACY-00000001", "GPTD-LEGACY", "plus", 100); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := DB.QueryRow(`SELECT code_kind FROM cardplatform_cdk_codes WHERE code LIKE 'GPTD-LEGACY%'`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != CodeKindLegacy {
		t.Fatalf("kind=%q want legacy", kind)
	}
}

func TestCachedDNCodeIsSiteNotLegacy(t *testing.T) {
	openTestDB(t)
	code := "DN-CACHED01-22223333-44445555"
	if err := SaveCardplatformCDKCode(0, code, "DN-CACHED01", "plus", 0); err != nil {
		t.Fatal(err)
	}
	row, ok := GetSiteCDKByCode(code)
	if !ok || row.CodeKind != CodeKindSite {
		t.Fatalf("cached DN code must be site/degraded, row=%+v ok=%v", row, ok)
	}
}

func TestListStoredCDKsDetailedFiltersAndMetadata(t *testing.T) {
	openTestDB(t)
	aID, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "主台 A", Protocol: AccountProtocolSpaceXLegacy,
		SiteBase: "https://a.example", CredSecret: "sk-a",
		Status: "active", Priority: 10, IsPrimaryDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	bID, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "备台 B", Protocol: AccountProtocolSpaceXLegacy,
		SiteBase: "https://b.example", CredSecret: "sk-b",
		Status: "active", Priority: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := CreatePendingSiteCDK("DN-LIST0001-22223333-44445555", "plus", true, 200)
	if err != nil {
		t.Fatal(err)
	}
	for i, accountID := range []int64{aID, bID} {
		if _, err := InsertSiteCDKBinding(SiteCDKBinding{
			SiteCodeID: row.ID, SiteCode: row.Code, AccountID: accountID,
			Provider: AccountProtocolSpaceXLegacy,
			RemoteID: fmt.Sprintf("remote-%d", i), RemoteCode: fmt.Sprintf("UP-%d", i),
			IsPrimary: i == 0, Status: BindingStatusUnused,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ActivateSiteCDK(row.ID, 200); err != nil {
		t.Fatal(err)
	}
	if err := MarkSiteCDKFulfilled(row.ID, bID, AccountProtocolSpaceXLegacy, true, "A timeout"); err != nil {
		t.Fatal(err)
	}

	list, total, err := ListStoredCDKsDetailed(StoredCDKListQuery{
		CodeKind: "site", BindingState: "complete",
		FulfilledAccount: bID, Failover: "yes", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d list=%+v", total, list)
	}
	got := list[0]
	if got.RowID != row.ID || got.CodeKind != CodeKindSite {
		t.Fatalf("unexpected row metadata: %+v", got)
	}
	if got.BindingTotal != 2 || got.BindingUsable != 2 {
		t.Fatalf("binding metadata: %+v", got)
	}
	if got.FulfilledAccount != "备台 B" || !got.FailoverUsed || got.FailoverReason != "A timeout" {
		t.Fatalf("fulfillment metadata: %+v", got)
	}

	_, none, err := ListStoredCDKsDetailed(StoredCDKListQuery{
		CodeKind: "site", BindingState: "degraded", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if none != 0 {
		t.Fatalf("complete binding matched degraded filter: %d", none)
	}
}

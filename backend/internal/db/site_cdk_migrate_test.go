package db

import (
	"fmt"
	"sync"
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
	ok, cid, err := ClaimBindingForRedeem(bindingID, "web-first-1")
	if err != nil || !ok {
		t.Fatalf("first claim ok=%v err=%v", ok, err)
	}
	if cid != "web-first-1" {
		t.Fatalf("first claim client id=%q, want the candidate", cid)
	}
	if ok, _, err := ClaimBindingForRedeem(bindingID, "web-second-2"); err != nil || ok {
		t.Fatalf("concurrent claim must fail: ok=%v err=%v", ok, err)
	}
	if _, err := DB.Exec(`
		UPDATE site_cdk_bindings SET updated_at=datetime('now', '-61 seconds') WHERE id=?
	`, bindingID); err != nil {
		t.Fatal(err)
	}
	// 租约过期后重试：必须复用第一次的键，否则上游会当成新兑换再扣一次
	ok, cid, err = ClaimBindingForRedeem(bindingID, "web-third-3")
	if err != nil || !ok {
		t.Fatalf("expired lease should retry: ok=%v err=%v", ok, err)
	}
	if cid != "web-first-1" {
		t.Fatalf("retry reused key=%q, want the original web-first-1", cid)
	}
}

// TestClaimBindingForRedeemConcurrentSingleWinner 是「同一毫秒并发双兑」双花猜想的直接回归防护。
// 兑换真正落到的 binding 是 ResolveSticky 粘定的那一张（active_binding_id），所以同一张本站码
// 的并发 redeem 都打向同一个 bindingID。这里用 24 个 goroutine 同时抢同一 binding，
// 断言「有且仅有一个认领成功」——其余必须被拒（409 的 DB 层来源）。
//
// 覆盖范围说明：本测试证明「单一 binding 维度」的并发互斥成立。跨台兄弟码的双兑窗口
// 另有两道防线（ResolveSticky 只认 active binding + PreviewWithFailover 对 redeeming 态拒绝切台
// + 事后 MarkConsumed/RetireSiblings 收回），属于 handler/provider 层，见那一侧的 mock 流程测试。
func TestClaimBindingForRedeemConcurrentSingleWinner(t *testing.T) {
	openTestDB(t)
	row, err := CreatePendingSiteCDK("DN-RACE1111-22223333-44445555", "plus", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	bindingID, err := InsertSiteCDKBinding(SiteCDKBinding{
		SiteCodeID: row.ID, SiteCode: row.Code, AccountID: 1,
		Provider: AccountProtocolSpaceXLegacy, RemoteID: "race-1",
		RemoteCode: "GPTD-RACE-1", IsPrimary: true, Status: BindingStatusUnused,
	})
	if err != nil {
		t.Fatal(err)
	}

	const n = 24 // 压满连接池（SetMaxOpenConns=25），逼出真实并发写
	var wg sync.WaitGroup
	start := make(chan struct{})
	var mu sync.Mutex
	wins := 0
	winnerKeys := map[string]struct{}{}
	var firstErr error
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // 一起起跑，尽量命中同一瞬间
			ok, cid, cerr := ClaimBindingForRedeem(bindingID, fmt.Sprintf("web-race-%d", i))
			mu.Lock()
			defer mu.Unlock()
			if cerr != nil && firstErr == nil {
				firstErr = cerr
			}
			if ok {
				wins++
				winnerKeys[cid] = struct{}{}
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("并发认领不应报错（WAL+busy_timeout 应吸收锁竞争）: %v", firstErr)
	}
	if wins != 1 {
		t.Fatalf("同一本站码并发兑换必须只有一个胜出，实得 %d 个（keys=%v）= 双花", wins, winnerKeys)
	}
	var status string
	if err := DB.QueryRow(`SELECT status FROM site_cdk_bindings WHERE id=?`, bindingID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != BindingStatusRedeeming {
		t.Fatalf("胜出后 binding 应为 redeeming，实为 %q", status)
	}
}

// TestClaimBindingForRedeemCodeLevelExclusive 覆盖「跨台兄弟码」双兑窄窗。
// 一张本站码在两台各有一张 unused 兄弟 binding。若并发的两次兑换分别落到不同 binding
// （preview 切台与 redeem 交错时可构造），binding 级 claim 各自为政会让两张都认领成功 = 跨台双花。
// 码级独占要求：同一 site_code_id 下的兄弟 binding 之间也只能有一个 claim 胜出。
func TestClaimBindingForRedeemCodeLevelExclusive(t *testing.T) {
	openTestDB(t)
	row, err := CreatePendingSiteCDK("DN-DUAL1111-22223333-44445555", "plus", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	// UNIQUE(site_code_id, account_id) 决定两张兄弟 binding 必须挂不同账户（= 两台）。
	bidA, err := InsertSiteCDKBinding(SiteCDKBinding{
		SiteCodeID: row.ID, SiteCode: row.Code, AccountID: 1,
		Provider: AccountProtocolSpaceXLegacy, RemoteID: "dual-A",
		RemoteCode: "GPTD-DUAL-A", IsPrimary: true, Status: BindingStatusUnused,
	})
	if err != nil {
		t.Fatal(err)
	}
	bidB, err := InsertSiteCDKBinding(SiteCDKBinding{
		SiteCodeID: row.ID, SiteCode: row.Code, AccountID: 2,
		Provider: AccountProtocolSpaceXLegacy, RemoteID: "dual-B",
		RemoteCode: "GPTD-DUAL-B", IsPrimary: false, Status: BindingStatusUnused,
	})
	if err != nil {
		t.Fatal(err)
	}

	targets := []int64{bidA, bidB}
	var wg sync.WaitGroup
	start := make(chan struct{})
	var mu sync.Mutex
	wins := 0
	var firstErr error
	for i, bid := range targets {
		wg.Add(1)
		go func(i int, bid int64) {
			defer wg.Done()
			<-start
			ok, _, cerr := ClaimBindingForRedeem(bid, fmt.Sprintf("web-dual-%d", i))
			mu.Lock()
			defer mu.Unlock()
			if cerr != nil && firstErr == nil {
				firstErr = cerr
			}
			if ok {
				wins++
			}
		}(i, bid)
	}
	close(start)
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("并发认领不应报错: %v", firstErr)
	}
	if wins != 1 {
		t.Fatalf("同一本站码跨台也必须只有一个 claim 胜出，实得 %d = 跨台双花", wins)
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

package db

import (
	"fmt"
	"testing"
)

func seedBinding(t *testing.T, code, remoteID string, status string) SiteCDKBinding {
	t.Helper()
	row, err := CreatePendingSiteCDK(code, "plus", true, 0)
	if err != nil {
		t.Fatalf("pending site cdk: %v", err)
	}
	id, err := InsertSiteCDKBinding(SiteCDKBinding{
		SiteCodeID: row.ID, SiteCode: row.Code, AccountID: 1,
		Provider: AccountProtocolSpaceXLegacy, RemoteID: remoteID,
		RemoteCode: "GPTD-" + remoteID, IsPrimary: true, Status: status,
	})
	if err != nil {
		t.Fatalf("insert binding: %v", err)
	}
	return SiteCDKBinding{ID: id, SiteCodeID: row.ID, RemoteID: remoteID}
}

// ClaimBindingForRetire 只允许一个协程认领；超期未完成的 retiring 必须能重新认领，
// 否则进程中途崩掉的那张码会永远停在 retiring，既不被重试也不再被路由。
func TestClaimBindingForRetireLeaseRecovery(t *testing.T) {
	openTestDB(t)
	b := seedBinding(t, "DN-RETIRE-1111-2222-3333", "retire-1", BindingStatusUnused)

	ok, err := ClaimBindingForRetire(b.ID)
	if err != nil || !ok {
		t.Fatalf("first claim ok=%v err=%v", ok, err)
	}
	if ok, _ := ClaimBindingForRetire(b.ID); ok {
		t.Fatal("second claim within lease must fail")
	}

	// 超过租约：视为上次收回中断，允许重来
	if _, err := DB.Exec(`
		UPDATE site_cdk_bindings SET updated_at = datetime('now', '-11 minutes') WHERE id = ?
	`, b.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := ClaimBindingForRetire(b.ID); err != nil || !ok {
		t.Fatalf("stale retiring should be reclaimable: ok=%v err=%v", ok, err)
	}
}

// reserved 的兄弟码也要能被认领收回：它不在可用态列表里，但同样需要处理。
func TestClaimBindingForRetireAcceptsReserved(t *testing.T) {
	openTestDB(t)
	b := seedBinding(t, "DN-RETIRE-4444-5555-6666", "retire-2", BindingStatusReserved)
	if ok, err := ClaimBindingForRetire(b.ID); err != nil || !ok {
		t.Fatalf("reserved claim ok=%v err=%v", ok, err)
	}
}

// 已定论的兄弟码不再重复处理。
func TestClaimBindingForRetireRejectsTerminal(t *testing.T) {
	openTestDB(t)
	for i, st := range []string{BindingStatusConsumed, BindingStatusRefunded, BindingStatusRetired} {
		b := seedBinding(t, fmt.Sprintf("DN-RETIRE-7777-8888-9%d%d%d", i, i, i), "retire-"+st, st)
		if ok, err := ClaimBindingForRetire(b.ID); err != nil || ok {
			t.Fatalf("%s should not be claimable: ok=%v err=%v", st, ok, err)
		}
	}
}

// 调用方没给幂等键时必须由服务端补一个，并在重试时复用同一个键；
// 留空会让每次重试都换成新键，上游当成两笔订单各扣一次。
func TestClaimBindingForRedeemGeneratesStableKeyWhenAbsent(t *testing.T) {
	openTestDB(t)
	b := seedBinding(t, "DN-KEY-1111-2222-3333", "key-1", BindingStatusUnused)

	ok, first, err := ClaimBindingForRedeem(b.ID, "")
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if first == "" {
		t.Fatal("server must mint a key when the caller sends none")
	}
	if ok, _, _ := ClaimBindingForRedeem(b.ID, ""); ok {
		t.Fatal("claim within lease must fail")
	}
	if _, err := DB.Exec(`
		UPDATE site_cdk_bindings SET updated_at = datetime('now', '-61 seconds') WHERE id = ?
	`, b.ID); err != nil {
		t.Fatal(err)
	}
	ok, second, err := ClaimBindingForRedeem(b.ID, "")
	if err != nil || !ok {
		t.Fatalf("reclaim ok=%v err=%v", ok, err)
	}
	if second != first {
		t.Fatalf("retry key=%q, want the minted %q", second, first)
	}
}

// 业务拒绝后清键：用户会换凭证重来，复用旧键会拿回同一个被拒的订单。
func TestResetBindingForRetryClearsKey(t *testing.T) {
	openTestDB(t)
	b := seedBinding(t, "DN-KEY-4444-5555-6666", "key-2", BindingStatusUnused)
	ok, key, err := ClaimBindingForRedeem(b.ID, "web-1")
	if err != nil || !ok || key != "web-1" {
		t.Fatalf("claim ok=%v key=%q err=%v", ok, key, err)
	}
	if err := ResetBindingForRetry(b.ID, "凭证无效"); err != nil {
		t.Fatal(err)
	}
	ok, key, err = ClaimBindingForRedeem(b.ID, "web-2")
	if err != nil || !ok {
		t.Fatalf("reclaim ok=%v err=%v", ok, err)
	}
	if key != "web-2" {
		t.Fatalf("key after reset = %q, want the new candidate", key)
	}
}

// 核销成功后键要被清掉，且只有第一个调用者算胜出。
func TestMarkBindingConsumedClearsKeyAndWinsOnce(t *testing.T) {
	openTestDB(t)
	b := seedBinding(t, "DN-KEY-7777-8888-9999", "key-3", BindingStatusUnused)
	if ok, _, err := ClaimBindingForRedeem(b.ID, "web-x"); err != nil || !ok {
		t.Fatalf("claim: %v", err)
	}
	won, err := MarkBindingConsumed(b.ID)
	if err != nil || !won {
		t.Fatalf("first consume won=%v err=%v", won, err)
	}
	var key, status string
	if err := DB.QueryRow(`SELECT redeem_client_id, status FROM site_cdk_bindings WHERE id = ?`, b.ID).Scan(&key, &status); err != nil {
		t.Fatal(err)
	}
	if key != "" || status != BindingStatusConsumed {
		t.Fatalf("key=%q status=%q", key, status)
	}
	if won, _ := MarkBindingConsumed(b.ID); won {
		t.Fatal("second consume must not win")
	}
}

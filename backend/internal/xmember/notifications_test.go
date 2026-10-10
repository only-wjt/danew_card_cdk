package xmember

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/avanfinity"
	"github.com/danew/cdk-recharge-system/internal/db"
)

func notificationDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "membership.db"))
	old := db.DB
	if err := db.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); db.DB = old })
}

func notificationAttempt(t *testing.T, account int64) (db.XCode, db.XRedemption) {
	t.Helper()
	c := db.XCode{Code: fmt.Sprintf("test-%d", account), Plan: "premium_3m", Channel: db.XChannelCDK, Status: "paying", AccountID: account}
	id, err := db.InsertXCode(c)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = id
	r := db.XRedemption{XCodeID: id, Channel: c.Channel, AccountID: account, Recipient: "recipient", ClientRequestID: "request", IdempotencyKey: "idem", NextPollAt: "2000-01-01 00:00:00"}
	r.ID, err = db.InsertXRedemption(r)
	if err != nil {
		t.Fatal(err)
	}
	return c, r
}

func taskCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestMembershipDurableSuccess(t *testing.T) {
	notificationDB(t)
	c, r := notificationAttempt(t, 1)
	if err := applyPublic(&c, &r, &avanfinity.PublicCDK{Status: "completed", Recipient: "recipient", AmountMinor: 1500, Currency: "usd"}, true); err != nil {
		t.Fatal(err)
	}
	if taskCount(t) != 0 {
		t.Fatal("status mapping must not enqueue before persistence")
	}
	if err := saveState(&c, &r); err != nil {
		t.Fatal(err)
	}
	persisted, err := db.GetXCode(c.ID)
	if err != nil || persisted.Status != "completed" {
		t.Fatalf("code=%+v err=%v", persisted, err)
	}
	red, err := db.GetXRedemption(r.ID)
	if err != nil || red.FinishedAt == "" || red.NextPollAt != "" {
		t.Fatalf("redemption=%+v err=%v", red, err)
	}
	var key, text string
	if err := db.DB.QueryRow(`SELECT business_key, text FROM telegram_notifications`).Scan(&key, &text); err != nil {
		t.Fatal(err)
	}
	if key != fmt.Sprintf("x:redemption:%d", r.ID) || !strings.Contains(text, "@recipient") || !strings.Contains(text, "1500 USD") {
		t.Fatalf("key=%s text=%s", key, text)
	}
	if err := saveState(&c, &r); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(context.Background(), r.ID, "completed", "confirmed"); err != nil {
		t.Fatal(err)
	}
	if taskCount(t) != 1 {
		t.Fatal("repeat generated duplicate")
	}
	c2, r2 := notificationAttempt(t, 2)
	mapStatus(&c2, &r2, "completed", nil)
	if err := saveState(&c2, &r2); err != nil {
		t.Fatal(err)
	}
	if taskCount(t) != 2 {
		t.Fatal("different accounts collided")
	}
}

func TestMembershipOutboxFailureRollback(t *testing.T) {
	notificationDB(t)
	c, r := notificationAttempt(t, 1)
	if _, err := db.DB.Exec(`CREATE TRIGGER reject_membership_task BEFORE INSERT ON telegram_notifications BEGIN SELECT RAISE(ABORT, 'storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	mapStatus(&c, &r, "completed", nil)
	if err := saveState(&c, &r); err == nil {
		t.Fatal("expected enqueue failure")
	}
	if c.Status != "paying" || r.FinishedAt != "" || r.NextPollAt == "" {
		t.Fatalf("not restored: code=%+v redemption=%+v", c, r)
	}
	// A trailing poll save must not accidentally finish failed completion.
	if err := saveState(&c, &r); err != nil {
		t.Fatal(err)
	}
	due, err := db.DueXRedemptions(20)
	if err != nil || len(due) != 1 {
		t.Fatalf("retry queue=%+v err=%v", due, err)
	}
	if taskCount(t) != 0 {
		t.Fatal("failed transaction leaked task")
	}
	if _, err := db.DB.Exec(`DROP TRIGGER reject_membership_task`); err != nil {
		t.Fatal(err)
	}
	mapStatus(&c, &r, "completed", nil)
	if err := saveState(&c, &r); err != nil {
		t.Fatal(err)
	}
	if taskCount(t) != 1 {
		t.Fatal("retry did not enqueue")
	}
}

func TestMembershipPendingFailureAndHistoryNoTask(t *testing.T) {
	for _, status := range []string{"processing", "paid_pending_delivery", "failed", "ineligible"} {
		t.Run(status, func(t *testing.T) {
			notificationDB(t)
			c, r := notificationAttempt(t, 1)
			mapStatus(&c, &r, status, nil)
			if err := saveState(&c, &r); err != nil {
				t.Fatal(err)
			}
			if taskCount(t) != 0 {
				t.Fatalf("%s generated success task", status)
			}
		})
	}
	t.Run("historical-completion", func(t *testing.T) {
		notificationDB(t)
		c, r := notificationAttempt(t, 1)
		if err := db.UpdateXCodeStatus(c.ID, "completed"); err != nil {
			t.Fatal(err)
		}
		c.Status = "completed"
		mapStatus(&c, &r, "completed", nil)
		if err := saveState(&c, &r); err != nil {
			t.Fatal(err)
		}
		if taskCount(t) != 0 {
			t.Fatal("historical completion replayed")
		}
	})
}

func TestDirectMembershipDurableSuccess(t *testing.T) {
	notificationDB(t)
	c, r := notificationAttempt(t, 1)
	c.Channel = db.XChannelDirect
	if err := applyDirect(&c, &r, &avanfinity.DirectOrder{Status: "completed", Recipient: "recipient", PaymentAttempted: true}); err != nil {
		t.Fatal(err)
	}
	if err := saveState(&c, &r); err != nil {
		t.Fatal(err)
	}
	if taskCount(t) != 1 {
		t.Fatal("direct success missing task")
	}
}

func TestManualMembershipDurableSuccess(t *testing.T) {
	notificationDB(t)
	c, r := notificationAttempt(t, 1)
	if err := Resolve(context.Background(), r.ID, "completed", "manual success"); err != nil {
		t.Fatal(err)
	}
	if taskCount(t) != 1 {
		t.Fatal("manual completion missing task")
	}
	persisted, err := db.GetXCode(c.ID)
	if err != nil || persisted.Status != "completed" {
		t.Fatalf("code=%+v err=%v", persisted, err)
	}
	if err := Resolve(context.Background(), r.ID, "completed", "updated note"); err != nil {
		t.Fatal(err)
	}
	red, err := db.GetXRedemption(r.ID)
	if err != nil || red.ResolvedNote != "updated note" {
		t.Fatalf("metadata compatibility: redemption=%+v err=%v", red, err)
	}
	if taskCount(t) != 1 {
		t.Fatal("manual repeat duplicated task")
	}
}

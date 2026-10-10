package db

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Open only an isolated temporary SQLite file: no Init, credentials, external
// services, or actual deployment database. Multiple connections exercise WAL locks.
func openTelegramNotificationTestDB(t *testing.T) string {
	t.Helper()
	previous := DB
	path := filepath.Join(t.TempDir(), "telegram.db")
	var err error
	DB, err = sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	DB.SetMaxOpenConns(16)
	t.Cleanup(func() {
		_ = DB.Close()
		DB = previous
	})
	if err := createTelegramNotificationTables(); err != nil {
		t.Fatal(err)
	}
	return path
}

func claimTelegramForTest(t *testing.T, now time.Time) *TelegramNotification {
	t.Helper()
	n, err := ClaimTelegramNotification(now, time.Minute)
	if err != nil || n == nil {
		t.Fatalf("claim = %+v, %v", n, err)
	}
	return n
}

func noTelegramDueForTest(t *testing.T, now time.Time) {
	t.Helper()
	n, err := ClaimTelegramNotification(now, time.Minute)
	if err != nil || n != nil {
		t.Fatalf("want no due notification, got %+v, %v", n, err)
	}
}

func TestTelegramNotificationDedupeAndTransactions(t *testing.T) {
	openTelegramNotificationTestDB(t)
	now := time.Now()
	for _, text := range []string{"original text", "replacement text"} {
		if err := EnqueueTelegramNotification("event:1", text); err != nil {
			t.Fatal(err)
		}
	}
	n := claimTelegramForTest(t, now)
	if n.Key != "event:1" || n.Text != "original text" || n.Attempts != 1 || n.LeaseToken == "" {
		t.Fatalf("unexpected delivery: %+v", n)
	}
	if err := MarkTelegramNotificationSent(n.ID, n.LeaseToken, now); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTelegramNotification("event:1", "after sent"); err != nil {
		t.Fatal(err)
	}
	noTelegramDueForTest(t, now.Add(time.Hour))

	for _, commit := range []bool{false, true} {
		tx, err := DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := EnqueueTelegramNotificationTx(tx, "transaction", "atomic text"); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		if !commit {
			noTelegramDueForTest(t, now)
		}
	}
	if n := claimTelegramForTest(t, now); n.Key != "transaction" {
		t.Fatalf("unexpected transaction delivery: %+v", n)
	}
}

func TestTelegramNotificationRetryIncludingMissingConfig(t *testing.T) {
	openTelegramNotificationTestDB(t)
	now := time.Now()
	if err := EnqueueTelegramNotification("config-failure", "retained body"); err != nil {
		t.Fatal(err)
	}
	// There is deliberately no terminal/dead-letter attempt limit. Configuration
	// failures follow the same persisted retry path as network/API failures.
	for attempt := 1; attempt <= 12; attempt++ {
		n := claimTelegramForTest(t, now)
		if n.Attempts != attempt || n.Text != "retained body" {
			t.Fatalf("attempt %d: %+v", attempt, n)
		}
		next := now.Add(time.Hour)
		if err := MarkTelegramNotificationRetry(n.ID, n.LeaseToken, next, "telegram configuration missing"); err != nil {
			t.Fatal(err)
		}
		var status, lastError string
		if err := DB.QueryRow(`SELECT status, last_error FROM telegram_notifications WHERE id=?`, n.ID).Scan(&status, &lastError); err != nil {
			t.Fatal(err)
		}
		if status != "retry" || lastError != "telegram configuration missing" {
			t.Fatalf("retry state %q, %q", status, lastError)
		}
		noTelegramDueForTest(t, next.Add(-time.Nanosecond))
		now = next
	}
	n := claimTelegramForTest(t, now)
	if err := MarkTelegramNotificationSent(n.ID, n.LeaseToken, now); err != nil {
		t.Fatal(err)
	}
	if err := MarkTelegramNotificationRetry(n.ID, n.LeaseToken, now, "late failure"); err != nil {
		t.Fatal(err)
	}
	noTelegramDueForTest(t, now.Add(time.Hour))
}

func TestTelegramNotificationConcurrentEnqueueAndClaim(t *testing.T) {
	openTelegramNotificationTestDB(t)
	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- EnqueueTelegramNotification("shared-event", "one body")
		}()
	}
	wg.Wait()
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("dedupe count %d: %v", count, err)
	}
	// More rows exercise concurrent leasing of different deliveries, not just a
	// single winner. Claims remain unacknowledged and must not be reclaimed early.
	for i := 1; i < 9; i++ {
		if err := EnqueueTelegramNotification(fmt.Sprintf("event:%d", i), "body"); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	claims := make(chan *TelegramNotification, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := ClaimTelegramNotification(now, time.Minute)
			claims <- n
			errs <- err
		}()
	}
	wg.Wait()
	seen := make(map[int64]bool)
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if n := <-claims; n != nil {
			if seen[n.ID] || n.Attempts != 1 {
				t.Fatalf("duplicate claim or attempts: %+v", n)
			}
			seen[n.ID] = true
		}
	}
	if len(seen) != 9 {
		t.Fatalf("claimed %d rows, want 9", len(seen))
	}
}

func TestTelegramNotificationRestartAndExpiredLeaseFencing(t *testing.T) {
	path := openTelegramNotificationTestDB(t)
	now := time.Now()
	if err := EnqueueTelegramNotification("restart", "survives restart"); err != nil {
		t.Fatal(err)
	}
	old := claimTelegramForTest(t, now)
	if err := DB.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	DB, err = sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := createTelegramNotificationTables(); err != nil {
		t.Fatal(err)
	}
	noTelegramDueForTest(t, now.Add(time.Minute-time.Nanosecond))
	current := claimTelegramForTest(t, now.Add(time.Minute))
	if current.ID != old.ID || current.LeaseToken == old.LeaseToken || current.Attempts != 2 || current.Text != old.Text {
		t.Fatalf("recovered delivery: %+v, old %+v", current, old)
	}
	if err := MarkTelegramNotificationSent(old.ID, old.LeaseToken, now); err != nil {
		t.Fatal(err)
	}
	if err := MarkTelegramNotificationRetry(old.ID, old.LeaseToken, now, "stale"); err != nil {
		t.Fatal(err)
	}
	var status, token string
	if err := DB.QueryRow(`SELECT status, lease_token FROM telegram_notifications WHERE id=?`, current.ID).Scan(&status, &token); err != nil {
		t.Fatal(err)
	}
	if status != "sending" || token != current.LeaseToken {
		t.Fatalf("stale owner overwrote new lease: %q %q", status, token)
	}
	if err := MarkTelegramNotificationSent(current.ID, current.LeaseToken, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	noTelegramDueForTest(t, now.Add(24*time.Hour))
}

func TestTelegramNotificationStorageErrorsAndTypedUniqueConstraint(t *testing.T) {
	openTelegramNotificationTestDB(t)
	if err := EnqueueTelegramNotification(" ", "text"); err == nil {
		t.Fatal("empty business key accepted")
	}
	if err := EnqueueTelegramNotificationTx(nil, "key", "text"); err == nil {
		t.Fatal("nil transaction accepted")
	}
	if _, err := ClaimTelegramNotification(time.Now(), 0); err == nil {
		t.Fatal("invalid lease accepted")
	}
	if _, err := DB.Exec(`CREATE TABLE unique_test (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`INSERT INTO unique_test VALUES (1,'one')`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`INSERT INTO unique_test VALUES (1,'two')`, `INSERT INTO unique_test VALUES (2,'one')`} {
		_, err := DB.Exec(query)
		if !IsUniqueConstraintError(fmt.Errorf("wrapped: %w", err)) {
			t.Fatalf("unique failure not recognized: %v", err)
		}
	}
	_, notNull := DB.Exec(`INSERT INTO unique_test VALUES (3,NULL)`)
	if notNull == nil || IsUniqueConstraintError(notNull) || IsUniqueConstraintError(nil) || IsUniqueConstraintError(errors.New("UNIQUE constraint failed")) {
		t.Fatal("non-unique errors misclassified")
	}
	// A different constraint failure must not be silently ignored by enqueue.
	if _, err := DB.Exec(`CREATE TRIGGER reject_telegram BEFORE INSERT ON telegram_notifications BEGIN SELECT RAISE(ABORT, 'storage rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTelegramNotification("reject", "text"); err == nil {
		t.Fatal("non-business-key storage error swallowed")
	}
	if _, err := DB.Exec(`DROP TABLE telegram_notifications`); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTelegramNotification("missing-table", "text"); err == nil {
		t.Fatal("missing table error swallowed")
	}
}

func TestTelegramNotificationCreateTablesDoesNotReplayHistory(t *testing.T) {
	openTelegramNotificationTestDB(t)
	// Explicit dummy test configuration prevents using any inherited credentials.
	t.Setenv("INSTALL_MODE", "wizard")
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("SETUP_BOOTSTRAP_TOKEN", "offline-test-only-token")
	if _, err := DB.Exec(`DROP TABLE telegram_notifications`); err != nil {
		t.Fatal(err)
	}
	if err := createTables(); err != nil {
		t.Fatal(err)
	}
	if err := InsertWebhookEvent(1, "completed", "historical-event", `{"order_id":"old"}`); err != nil {
		t.Fatal(err)
	}
	if err := createTables(); err != nil {
		t.Fatal(err)
	}
	noTelegramDueForTest(t, time.Now().Add(time.Hour))
	var count int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM webhook_events`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("historical webhook record changed: count=%d err=%v", count, err)
	}
}

func TestTelegramNotificationAliasesEnrichPendingAndSent(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprintf("sent=%v", sent), func(t *testing.T) {
			openTelegramNotificationTestDB(t)
			fallback, canonical := "account:1|client:legacy", "account:1|order:42"
			if err := EnqueueTelegramNotification(fallback, "original"); err != nil {
				t.Fatal(err)
			}
			if sent {
				n := claimTelegramForTest(t, time.Now())
				if err := MarkTelegramNotificationSent(n.ID, n.LeaseToken, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if err := EnqueueTelegramNotificationAliases([]string{canonical, fallback, "account:1|code:abc", fallback}, "richer text"); err != nil {
				t.Fatal(err)
			}
			// Canonical-only subsequent calls, including the original single-key
			// API, must find the fallback-owned row even after delivery.
			if err := EnqueueTelegramNotification(canonical, "canonical text"); err != nil {
				t.Fatal(err)
			}
			if err := EnqueueTelegramNotificationAliases([]string{canonical}, "again"); err != nil {
				t.Fatal(err)
			}
			var count, aliases int
			if err := DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("notification count=%d err=%v", count, err)
			}
			if err := DB.QueryRow(`SELECT COUNT(*) FROM telegram_notification_aliases`).Scan(&aliases); err != nil || aliases != 3 {
				t.Fatalf("alias count=%d err=%v", aliases, err)
			}
			if sent {
				noTelegramDueForTest(t, time.Now().Add(time.Hour))
			} else if n := claimTelegramForTest(t, time.Now()); n.Key != fallback || n.Text != "original" {
				t.Fatalf("original row changed: %+v", n)
			}
			// Identical identifiers in a different account remain independent.
			if err := EnqueueTelegramNotificationAliases([]string{"account:2|order:42", "account:2|client:legacy"}, "other account"); err != nil {
				t.Fatal(err)
			}
			if n := claimTelegramForTest(t, time.Now()); n.Text != "other account" {
				t.Fatalf("cross-account alias collision: %+v", n)
			}
		})
	}
}

func TestTelegramNotificationAliasesConcurrentAssociation(t *testing.T) {
	openTelegramNotificationTestDB(t)
	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			keys := []string{fmt.Sprintf("account:1|observation:%d", i), "account:1|client:shared"}
			if i%2 == 0 {
				keys[0], keys[1] = keys[1], keys[0]
			}
			errs <- EnqueueTelegramNotificationAliases(keys, "body")
		}(i)
	}
	wg.Wait()
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	var count, owners, aliases int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("notification count=%d err=%v", count, err)
	}
	if err := DB.QueryRow(`SELECT COUNT(DISTINCT notification_id), COUNT(*) FROM telegram_notification_aliases`).Scan(&owners, &aliases); err != nil || owners != 1 || aliases != workers+1 {
		t.Fatalf("owners=%d aliases=%d err=%v", owners, aliases, err)
	}
}

func TestTelegramNotificationAliasesRollbackAndAmbiguity(t *testing.T) {
	openTelegramNotificationTestDB(t)
	if _, err := DB.Exec(`CREATE TRIGGER reject_alias BEFORE INSERT ON telegram_notification_aliases
		WHEN NEW.alias_key = 'rejected' BEGIN SELECT RAISE(ABORT, 'alias storage rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTelegramNotificationAliases([]string{"new", "rejected"}, "body"); err == nil {
		t.Fatal("alias storage error swallowed")
	}
	var count int
	for _, table := range []string{"telegram_notifications", "telegram_notification_aliases"} {
		if err := DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial write in %s: count=%d err=%v", table, count, err)
		}
	}
	// A failed Tx enqueue is rolled back to its savepoint even if its caller
	// elects to commit other business changes rather than rolling back the Tx.
	tx, err := DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTelegramNotificationTx(tx, "rejected", "body"); err == nil {
		_ = tx.Rollback()
		t.Fatal("Tx alias storage error swallowed")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("Tx partial notification: count=%d err=%v", count, err)
	}
	for _, key := range []string{"fallback", "canonical"} {
		if err := EnqueueTelegramNotification(key, "already separate"); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnqueueTelegramNotificationAliases([]string{"fresh", "fallback", "canonical"}, "ambiguous"); err == nil {
		t.Fatal("different existing owners silently merged")
	}
	if err := DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("ambiguity changed notifications: count=%d err=%v", count, err)
	}
	if err := DB.QueryRow(`SELECT COUNT(*) FROM telegram_notification_aliases`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("ambiguity partially attached aliases: count=%d err=%v", count, err)
	}
}

func TestTelegramNotificationAliasesRecognizePreAliasQueueRows(t *testing.T) {
	openTelegramNotificationTestDB(t)
	if _, err := DB.Exec(`INSERT INTO telegram_notifications (business_key,text,status,created_at) VALUES ('fallback','original','sent',0)`); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTelegramNotificationAliases([]string{"canonical", "fallback"}, "new"); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTelegramNotification("canonical", "again"); err != nil {
		t.Fatal(err)
	}
	noTelegramDueForTest(t, time.Now())
	for _, keys := range [][]string{nil, {}, {"valid", " "}} {
		if err := EnqueueTelegramNotificationAliases(keys, "invalid"); err == nil {
			t.Fatalf("invalid aliases accepted: %v", keys)
		}
	}
}

package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

// TelegramNotification is a claimed delivery. LeaseToken must accompany every
// acknowledgement; Attempts includes the current claim (starting at one).
// Only message text is persisted, never sender configuration or bot credentials.
type TelegramNotification struct {
	ID         int64
	Key        string
	Text       string
	LeaseToken string
	Attempts   int
}

func createTelegramNotificationTables() error {
	for _, query := range []string{
		`CREATE TABLE IF NOT EXISTS telegram_notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			business_key TEXT NOT NULL UNIQUE,
			text TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','retry')),
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL DEFAULT 0,
			lease_until INTEGER NOT NULL DEFAULT 0,
			lease_token TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			sent_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_telegram_notifications_due ON telegram_notifications(status, next_attempt_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_telegram_notifications_lease ON telegram_notifications(status, lease_until, id)`,
		`CREATE TABLE IF NOT EXISTS telegram_notification_aliases (
			alias_key TEXT PRIMARY KEY NOT NULL,
			notification_id INTEGER NOT NULL REFERENCES telegram_notifications(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_telegram_notification_alias_owner ON telegram_notification_aliases(notification_id)`,
	} {
		if _, err := DB.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

// IsUniqueConstraintError recognizes SQLite UNIQUE/PRIMARY KEY failures,
// including wrapped driver errors, without relying on error message strings.
func IsUniqueConstraintError(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) &&
		(sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique || sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey)
}

// EnqueueTelegramNotification durably inserts a new business event exactly once.
// Single-key callers also honor aliases learned by richer observations.
func EnqueueTelegramNotification(key, text string) error {
	return EnqueueTelegramNotificationAliases([]string{key}, text)
}

// EnqueueTelegramNotificationAliases atomically associates all supplied business
// identities with one delivery. Callers must qualify keys by account and identity
// kind and include a shared alias when enriching a previous observation. The first
// key is the business_key for a new row; existing text, leases and sent state stay
// unchanged. Conflicting owners return an error without merging or dropping rows:
// disjoint observations queued before association may already have been delivered.
// Only this queue is consulted, never historical webhook/notice records.
func EnqueueTelegramNotificationAliases(keys []string, text string) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := enqueueTelegramNotificationAliasesTx(tx, keys, text); err != nil {
		return err
	}
	return tx.Commit()
}

func enqueueTelegramNotificationAliasesTx(tx *sql.Tx, keys []string, text string) (err error) {
	if len(keys) == 0 {
		return fmt.Errorf("telegram notification business keys required")
	}
	unique := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("telegram notification business key required")
		}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, key)
		}
	}
	// A savepoint makes even the caller-owned Tx API all-or-nothing on errors.
	if _, err = tx.Exec(`SAVEPOINT telegram_notification_enqueue`); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_, _ = tx.Exec(`ROLLBACK TO telegram_notification_enqueue`)
			_, _ = tx.Exec(`RELEASE telegram_notification_enqueue`)
		} else {
			_, err = tx.Exec(`RELEASE telegram_notification_enqueue`)
		}
	}()
	// Acquire SQLite's writer lock before looking up aliases. A write statement
	// (even matching zero rows) avoids the deferred read-to-write upgrade race.
	if _, err = tx.Exec(`UPDATE telegram_notifications SET id = id WHERE 0`); err != nil {
		return err
	}
	var owner int64
	for _, key := range unique {
		// Direct business_key lookup also recognizes durable queue rows created
		// before alias support, without backfilling or replaying old notices.
		rows, queryErr := tx.Query(`SELECT notification_id FROM telegram_notification_aliases WHERE alias_key = ?
			UNION SELECT id FROM telegram_notifications WHERE business_key = ?`, key, key)
		if queryErr != nil {
			return queryErr
		}
		for rows.Next() {
			var id int64
			if scanErr := rows.Scan(&id); scanErr != nil {
				_ = rows.Close()
				return scanErr
			}
			if owner != 0 && owner != id {
				_ = rows.Close()
				return fmt.Errorf("ambiguous telegram notification aliases: multiple notification owners")
			}
			owner = id
		}
		queryErr = rows.Err()
		closeErr := rows.Close()
		if queryErr != nil {
			return queryErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if owner == 0 {
		result, insertErr := tx.Exec(`INSERT INTO telegram_notifications (business_key, text, created_at)
			VALUES (?, ?, ?)`, unique[0], text, time.Now().UnixNano())
		if insertErr != nil {
			return insertErr
		}
		owner, err = result.LastInsertId()
		if err != nil {
			return err
		}
	}
	for _, key := range unique {
		// All owners were checked under the writer lock. Ignore only this exact
		// alias-key duplicate; other storage and constraint errors propagate.
		if _, err = tx.Exec(`INSERT INTO telegram_notification_aliases (alias_key, notification_id)
			VALUES (?, ?) ON CONFLICT(alias_key) DO NOTHING`, key, owner); err != nil {
			return err
		}
	}
	return nil
}

// EnqueueTelegramNotificationTx participates in the caller's transaction. It
// neither commits nor rolls it back, allowing an atomic business-state/outbox write.
func EnqueueTelegramNotificationTx(tx *sql.Tx, key, text string) error {
	if tx == nil {
		return fmt.Errorf("telegram notification transaction required")
	}
	return enqueueTelegramNotificationAliasesTx(tx, []string{key}, text)
}

// ClaimTelegramNotification atomically leases one due row, recovering expired
// sending rows after crashes. Times are Unix nanoseconds to retain lease precision.
// Retries have no attempt limit; the sender chooses its capped backoff schedule.
func ClaimTelegramNotification(now time.Time, lease time.Duration) (*TelegramNotification, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	if lease <= 0 {
		return nil, fmt.Errorf("telegram notification lease must be positive")
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	n := new(TelegramNotification)
	// One write statement, not a select-then-update transaction: concurrent
	// connections cannot claim the same unexpired lease.
	err := DB.QueryRow(`UPDATE telegram_notifications
		SET status = 'sending', attempts = attempts + 1, lease_token = ?, lease_until = ?
		WHERE id = (
			SELECT id FROM telegram_notifications
			WHERE (status IN ('pending','retry') AND next_attempt_at <= ?)
			   OR (status = 'sending' AND lease_until <= ?)
			ORDER BY id LIMIT 1
		)
		RETURNING id, business_key, text, lease_token, attempts`,
		hex.EncodeToString(token[:]), now.Add(lease).UnixNano(), now.UnixNano(), now.UnixNano()).
		Scan(&n.ID, &n.Key, &n.Text, &n.LeaseToken, &n.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

// MarkTelegramNotificationSent acknowledges only the current owner. Stale or
// repeated acknowledgements are harmless no-ops, never overwriting a newer lease.
func MarkTelegramNotificationSent(id int64, leaseToken string, now time.Time) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	_, err := DB.Exec(`UPDATE telegram_notifications
		SET status = 'sent', sent_at = ?, lease_token = '', lease_until = 0, last_error = ''
		WHERE id = ? AND status = 'sending' AND lease_token = ? AND lease_token != ''`,
		now.UnixNano(), id, leaseToken)
	return err
}

// MarkTelegramNotificationRetry releases the current lease and schedules another
// attempt, including ordinary failures caused by missing sender configuration.
// lastError must be sanitized by the sender to exclude bot credentials.
func MarkTelegramNotificationRetry(id int64, leaseToken string, next time.Time, lastError string) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	_, err := DB.Exec(`UPDATE telegram_notifications
		SET status = 'retry', next_attempt_at = ?, last_error = ?, lease_token = '', lease_until = 0
		WHERE id = ? AND status = 'sending' AND lease_token = ? AND lease_token != ''`,
		next.UnixNano(), lastError, id, leaseToken)
	return err
}

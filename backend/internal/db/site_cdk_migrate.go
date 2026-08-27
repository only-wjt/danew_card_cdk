package db

import (
	"fmt"
	"log"
)

// migrateSiteDualBindSchema 演进 cardplatform_cdk_codes（加 id / code_kind 等）并建 binding 表。
func migrateSiteDualBindSchema() error {
	if DB == nil {
		return nil
	}
	if err := migrateCardPlatformAccounts(); err != nil {
		return err
	}
	if err := migrateAccountCardSelection(); err != nil {
		return err
	}
	if err := migrateSiteCDKBindings(); err != nil {
		return err
	}
	return migrateCardplatformCDKCodesWithID()
}

func migrateCardplatformCDKCodesWithID() error {
	var hasID int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cardplatform_cdk_codes') WHERE name='id'`).Scan(&hasID); err != nil {
		return err
	}
	if hasID > 0 {
		return ensureCardplatformCDKExtraCols()
	}
	log.Println("migrating cardplatform_cdk_codes: rebuild with id PK for dual-bind")
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		CREATE TABLE cardplatform_cdk_codes_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			upstream_id INTEGER,
			code TEXT NOT NULL UNIQUE,
			code_prefix TEXT,
			plan TEXT,
			fee_amount_minor INTEGER DEFAULT 0,
			status TEXT DEFAULT '',
			assigned_agent_user_id INTEGER DEFAULT 0,
			code_kind TEXT NOT NULL DEFAULT 'legacy',
			remote_id TEXT NOT NULL DEFAULT '',
			issue_status TEXT NOT NULL DEFAULT 'active',
			dual_eligible INTEGER NOT NULL DEFAULT 0,
			fulfilled_account_id INTEGER NOT NULL DEFAULT 0,
			fulfilled_provider TEXT NOT NULL DEFAULT '',
			failover_used INTEGER NOT NULL DEFAULT 0,
			failover_reason TEXT NOT NULL DEFAULT '',
			active_binding_id INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create new cdk table: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO cardplatform_cdk_codes_new
		(upstream_id, code, code_prefix, plan, fee_amount_minor, status, assigned_agent_user_id,
		 code_kind, remote_id, issue_status, dual_eligible, created_at)
		SELECT
			upstream_id, code, code_prefix, plan, fee_amount_minor, status,
			COALESCE(assigned_agent_user_id, 0),
			CASE
				WHEN lower(trim(COALESCE(plan,''))) = 'gpt_white' THEN 'local_stock'
				ELSE 'legacy'
			END,
			CASE WHEN upstream_id IS NOT NULL THEN CAST(upstream_id AS TEXT) ELSE '' END,
			'active',
			0,
			created_at
		FROM cardplatform_cdk_codes
	`); err != nil {
		return fmt.Errorf("copy cdk rows: %w", err)
	}

	if _, err := tx.Exec(`DROP TABLE cardplatform_cdk_codes`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE cardplatform_cdk_codes_new RENAME TO cardplatform_cdk_codes`); err != nil {
		return err
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_cp_cdk_upstream ON cardplatform_cdk_codes(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cp_cdk_prefix ON cardplatform_cdk_codes(code_prefix)`,
		`CREATE INDEX IF NOT EXISTS idx_cp_cdk_assigned_agent ON cardplatform_cdk_codes(assigned_agent_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cp_cdk_kind ON cardplatform_cdk_codes(code_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_cp_cdk_issue ON cardplatform_cdk_codes(issue_status)`,
	}
	for _, q := range indexes {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureCardplatformCDKExtraCols() error {
	cols := []struct {
		name string
		ddl  string
	}{
		{"code_kind", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN code_kind TEXT NOT NULL DEFAULT 'legacy'`},
		{"remote_id", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN remote_id TEXT NOT NULL DEFAULT ''`},
		{"issue_status", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN issue_status TEXT NOT NULL DEFAULT 'active'`},
		{"dual_eligible", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN dual_eligible INTEGER NOT NULL DEFAULT 0`},
		{"fulfilled_account_id", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN fulfilled_account_id INTEGER NOT NULL DEFAULT 0`},
		{"fulfilled_provider", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN fulfilled_provider TEXT NOT NULL DEFAULT ''`},
		{"failover_used", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN failover_used INTEGER NOT NULL DEFAULT 0`},
		{"failover_reason", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN failover_reason TEXT NOT NULL DEFAULT ''`},
		{"active_binding_id", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN active_binding_id INTEGER NOT NULL DEFAULT 0`},
	}
	for _, c := range cols {
		var n int
		if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cardplatform_cdk_codes') WHERE name=?`, c.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := DB.Exec(c.ddl); err != nil {
				return err
			}
		}
	}
	_, _ = DB.Exec(`
		UPDATE cardplatform_cdk_codes
		SET code_kind = 'local_stock'
		WHERE lower(trim(COALESCE(plan,''))) = 'gpt_white'
		  AND (code_kind = '' OR code_kind = 'legacy')
	`)
	return nil
}

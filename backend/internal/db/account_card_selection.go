package db

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 多卡台选卡缓存使用独立表，避免老表 plan_key/product_code 单列主键导致 A/B 相互覆盖。
func migrateAccountCardSelection() error {
	if DB == nil {
		return nil
	}
	ddls := []string{
		`CREATE TABLE IF NOT EXISTS account_card_selection_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id INTEGER NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			plan_key TEXT NOT NULL,
			display_name TEXT NOT NULL,
			bin_prefix TEXT DEFAULT '',
			channel TEXT DEFAULT '',
			enabled INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_acsr_account_sort
			ON account_card_selection_rules(account_id, sort_order, id)`,
		`CREATE TABLE IF NOT EXISTS account_plan_status_cache (
			account_id INTEGER NOT NULL,
			plan_key TEXT NOT NULL,
			label TEXT DEFAULT '',
			online INTEGER DEFAULT 1,
			service_fee_usd_minor INTEGER DEFAULT 0,
			synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(account_id, plan_key)
		)`,
		`CREATE TABLE IF NOT EXISTS account_card_product_cache (
			account_id INTEGER NOT NULL,
			product_code TEXT NOT NULL,
			issuer TEXT DEFAULT '',
			bin TEXT DEFAULT '',
			network TEXT DEFAULT '',
			issuing_area TEXT DEFAULT '',
			scene TEXT DEFAULT '',
			card_group TEXT DEFAULT '',
			description TEXT DEFAULT '',
			bin_heads TEXT DEFAULT '',
			enabled INTEGER DEFAULT 1,
			suspended_at TEXT DEFAULT '',
			synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(account_id, product_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_acpc_account_enabled
			ON account_card_product_cache(account_id, enabled)`,
		`CREATE TABLE IF NOT EXISTS account_card_fail_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id INTEGER NOT NULL,
			card_id INTEGER NOT NULL,
			card_last_four TEXT DEFAULT '',
			order_id INTEGER NOT NULL DEFAULT 0,
			cdk_code TEXT DEFAULT '',
			account_email_norm TEXT NOT NULL DEFAULT '',
			email_source TEXT DEFAULT '',
			error_code TEXT DEFAULT '',
			order_status TEXT DEFAULT '',
			verdict TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(account_id, order_id, card_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_acfe_card
			ON account_card_fail_events(account_id, card_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS account_card_blocklist (
			account_id INTEGER NOT NULL,
			card_id INTEGER NOT NULL,
			card_last_four TEXT DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			distinct_emails INTEGER NOT NULL DEFAULT 0,
			fail_count INTEGER NOT NULL DEFAULT 0,
			freeze_status TEXT DEFAULT '',
			freeze_error TEXT DEFAULT '',
			blocked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			unblocked_at DATETIME,
			notes TEXT DEFAULT '',
			PRIMARY KEY(account_id, card_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_acbl_active
			ON account_card_blocklist(account_id, blocked_at) WHERE unblocked_at IS NULL`,
	}
	for _, ddl := range ddls {
		if _, err := DB.Exec(ddl); err != nil {
			return err
		}
	}
	return seedPrimaryAccountCardSelection()
}

// 首次升级把现有单台配置复制给主台；后续每台独立维护，不再互相覆盖。
func seedPrimaryAccountCardSelection() error {
	if done, _ := GetSetting("account_scoping_seeded_v1"); strings.TrimSpace(done) == "1" {
		return nil
	}
	var accountID int64
	err := DB.QueryRow(`
		SELECT id FROM card_platform_accounts
		ORDER BY is_primary_default DESC, priority ASC, id ASC LIMIT 1
	`).Scan(&accountID)
	if err != nil || accountID <= 0 {
		return nil
	}
	if _, err := DB.Exec(`
		INSERT INTO account_card_selection_rules
			(account_id, sort_order, plan_key, display_name, bin_prefix, channel, enabled, created_at)
		SELECT ?, sort_order, plan_key, display_name, bin_prefix, channel, enabled, created_at
		FROM card_selection_rules
		WHERE NOT EXISTS (
			SELECT 1 FROM account_card_selection_rules x WHERE x.account_id = ?
		)
	`, accountID, accountID); err != nil {
		return err
	}
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO account_plan_status_cache
			(account_id, plan_key, label, online, service_fee_usd_minor, synced_at)
		SELECT ?, plan_key, label, online, service_fee_usd_minor, synced_at FROM plan_status_cache
	`, accountID); err != nil {
		return err
	}
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO account_card_fail_events
			(account_id, card_id, card_last_four, order_id, cdk_code, account_email_norm,
			 email_source, error_code, order_status, verdict, created_at)
		SELECT ?, card_id, card_last_four, order_id, cdk_code, account_email_norm,
		       email_source, error_code, order_status, verdict, created_at
		FROM card_fail_events
	`, accountID); err != nil {
		return err
	}
	if _, err := DB.Exec(`
		INSERT OR IGNORE INTO account_card_blocklist
			(account_id, card_id, card_last_four, reason, distinct_emails, fail_count,
			 freeze_status, freeze_error, blocked_at, unblocked_at, notes)
		SELECT ?, card_id, card_last_four, reason, distinct_emails, fail_count,
		       freeze_status, freeze_error, blocked_at, unblocked_at, notes
		FROM card_blocklist
	`, accountID); err != nil {
		return err
	}
	if _, err = DB.Exec(`
		INSERT OR IGNORE INTO account_card_product_cache
			(account_id, product_code, issuer, bin, network, issuing_area, scene, card_group,
			 description, bin_heads, enabled, suspended_at, synced_at)
		SELECT ?, product_code, issuer, bin, network, issuing_area, scene, card_group,
		       description, bin_heads, enabled, suspended_at, synced_at
		FROM card_product_cache
	`, accountID); err != nil {
		return err
	}
	return SetSetting("account_scoping_seeded_v1", "1")
}

func GetCardSelectionRulesForAccount(accountID int64) ([]CardSelectionRule, error) {
	if DB == nil || accountID <= 0 {
		return nil, fmt.Errorf("invalid account id")
	}
	rows, err := DB.Query(`
		SELECT id, sort_order, plan_key, display_name,
		       COALESCE(bin_prefix,''), COALESCE(channel,''),
		       enabled, COALESCE(created_at,'')
		FROM account_card_selection_rules
		WHERE account_id = ?
		ORDER BY sort_order ASC, id ASC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CardSelectionRule, 0)
	for rows.Next() {
		var r CardSelectionRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.SortOrder, &r.PlanKey, &r.DisplayName,
			&r.BinPrefix, &r.Channel, &enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.AccountID = accountID
		r.Enabled = enabled != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func SetCardSelectionRulesForAccount(accountID int64, rules []CardSelectionRule) error {
	if DB == nil || accountID <= 0 {
		return fmt.Errorf("invalid account id")
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM account_card_selection_rules WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	for i, r := range rules {
		sortOrder := r.SortOrder
		if sortOrder <= 0 {
			sortOrder = i + 1
		}
		enabled := 0
		if r.Enabled {
			enabled = 1
		}
		if _, err := tx.Exec(`
			INSERT INTO account_card_selection_rules
				(account_id, sort_order, plan_key, display_name, bin_prefix, channel, enabled)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, accountID, sortOrder, strings.TrimSpace(r.PlanKey), strings.TrimSpace(r.DisplayName),
			strings.TrimSpace(r.BinPrefix), strings.TrimSpace(r.Channel), enabled); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func GetPlanStatusCacheForAccount(accountID int64) ([]PlanStatusCache, error) {
	rows, err := DB.Query(`
		SELECT plan_key, COALESCE(label,''), online,
		       COALESCE(service_fee_usd_minor,0), COALESCE(synced_at,'')
		FROM account_plan_status_cache WHERE account_id = ? ORDER BY plan_key
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PlanStatusCache, 0)
	for rows.Next() {
		var p PlanStatusCache
		var online int
		if err := rows.Scan(&p.PlanKey, &p.Label, &online, &p.ServiceFeeUsdMinor, &p.SyncedAt); err != nil {
			return nil, err
		}
		p.AccountID = accountID
		p.Online = online != 0
		p.ServiceFeeUSD = float64(p.ServiceFeeUsdMinor) / 100
		out = append(out, p)
	}
	return out, rows.Err()
}

func GetPlanStatusCacheMapForAccount(accountID int64) (map[string]PlanStatusCache, error) {
	list, err := GetPlanStatusCacheForAccount(accountID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]PlanStatusCache, len(list))
	for _, p := range list {
		out[p.PlanKey] = p
	}
	return out, nil
}

func UpsertPlanStatusForAccount(accountID int64, planKey, label string, online bool, feeMinor int64) error {
	on := 0
	if online {
		on = 1
	}
	_, err := DB.Exec(`
		INSERT INTO account_plan_status_cache
			(account_id, plan_key, label, online, service_fee_usd_minor, synced_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(account_id, plan_key) DO UPDATE SET
			label=excluded.label, online=excluded.online,
			service_fee_usd_minor=excluded.service_fee_usd_minor,
			synced_at=CURRENT_TIMESTAMP
	`, accountID, strings.TrimSpace(planKey), label, on, feeMinor)
	return err
}

func UpsertCardProductForAccount(accountID int64, p CardProductCache) error {
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	_, err := DB.Exec(`
		INSERT INTO account_card_product_cache
			(account_id, product_code, issuer, bin, network, issuing_area, scene, card_group,
			 description, bin_heads, enabled, suspended_at, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(account_id, product_code) DO UPDATE SET
			issuer=excluded.issuer, bin=excluded.bin, network=excluded.network,
			issuing_area=excluded.issuing_area, scene=excluded.scene, card_group=excluded.card_group,
			description=excluded.description, bin_heads=excluded.bin_heads, enabled=excluded.enabled,
			suspended_at=excluded.suspended_at, synced_at=CURRENT_TIMESTAMP
	`, accountID, p.ProductCode, p.Issuer, p.BIN, p.Network, p.IssuingArea, p.Scene,
		p.CardGroup, p.Description, encodeCardSelectionStrings(p.BinHeads), enabled, p.SuspendedAt)
	return err
}

func GetCardProductsForAccount(accountID int64) ([]CardProductCache, error) {
	rows, err := DB.Query(`
		SELECT product_code, COALESCE(issuer,''), COALESCE(bin,''), COALESCE(network,''),
		       COALESCE(issuing_area,''), COALESCE(scene,''), COALESCE(card_group,''),
		       COALESCE(description,''), COALESCE(bin_heads,'[]'), enabled,
		       COALESCE(suspended_at,''), COALESCE(synced_at,'')
		FROM account_card_product_cache
		WHERE account_id = ?
		ORDER BY enabled DESC, product_code ASC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CardProductCache, 0)
	for rows.Next() {
		var p CardProductCache
		var binHeads string
		var enabled int
		if err := rows.Scan(&p.ProductCode, &p.Issuer, &p.BIN, &p.Network,
			&p.IssuingArea, &p.Scene, &p.CardGroup, &p.Description,
			&binHeads, &enabled, &p.SuspendedAt, &p.SyncedAt); err != nil {
			return nil, err
		}
		p.AccountID = accountID
		p.Enabled = enabled != 0
		_ = json.Unmarshal([]byte(binHeads), &p.BinHeads)
		out = append(out, p)
	}
	return out, rows.Err()
}

func encodeCardSelectionStrings(values []string) string {
	raw, _ := json.Marshal(values)
	return string(raw)
}

func MarkCardProductsOfflineExceptForAccount(accountID int64, present map[string]bool) (int, error) {
	products, err := GetCardProductsForAccount(accountID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range products {
		if present[p.ProductCode] || !p.Enabled {
			continue
		}
		res, err := DB.Exec(`
			UPDATE account_card_product_cache
			SET enabled=0, synced_at=CURRENT_TIMESTAMP
			WHERE account_id=? AND product_code=? AND enabled=1
		`, accountID, p.ProductCode)
		if err != nil {
			return n, err
		}
		if aff, _ := res.RowsAffected(); aff > 0 {
			n++
		}
	}
	return n, nil
}

// PreferredCardSelectionForAccount 双发时使用该卡台自己的首条启用规则。
func PreferredCardSelectionForAccount(accountID int64) (issuer, segmentType, segmentKey string) {
	rules, err := GetCardSelectionRulesForAccount(accountID)
	if err != nil {
		return "", "", ""
	}
	for _, r := range rules {
		if !r.Enabled || strings.TrimSpace(r.PlanKey) == "" {
			continue
		}
		issuer = strings.ToLower(strings.TrimSpace(r.Channel))
		return issuer, "product", strings.TrimSpace(r.PlanKey)
	}
	return "", "", ""
}

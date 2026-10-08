package db

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	XChannelCDK    = "x_cdk"
	XChannelDirect = "x_direct"
)

// XCardPref 是已有卡池里的一张，以及它是否参与自动选。
type XCardPref struct {
	ID      int64 `json:"id"`
	Enabled bool  `json:"enabled"`
}

// XChannel 是一条 X 会员通道。一个通道同一时间只绑一个卡台。
// PayMode：existing 从已有卡按顺序自动选，fixed 钉死 CardID，new 每笔开新卡。
type XChannel struct {
	Channel           string      `json:"channel"`
	AccountID         int64       `json:"account_id"`
	Enabled           bool        `json:"enabled"`
	CardID            int64       `json:"card_id"`
	AutoCard          bool        `json:"auto_card"`
	AutoCardProduct   string      `json:"auto_card_product"`
	AutoCardFirstName string      `json:"auto_card_first_name"`
	AutoCardLastName  string      `json:"auto_card_last_name"`
	PayMode           string      `json:"pay_mode"`
	PayFallback       bool        `json:"pay_fallback"`
	CardOrder         string      `json:"-"`
	CardPrefs         []XCardPref `json:"card_order"`
	UpdatedAt         string      `json:"updated_at"`
}

// XPlanLimit 是某个通道、某个套餐的花费上限。金额用十进制字符串，避免浮点。
type XPlanLimit struct {
	Channel                string `json:"channel"`
	Plan                   string `json:"plan"`
	Enabled                bool   `json:"enabled"`
	Currency               string `json:"currency"`
	MaxOfficialAmountMinor int64  `json:"max_official_amount_minor"`
	MaxServiceFeeUSD       string `json:"max_service_fee_usd"`
	MaxWalletDebitUSD      string `json:"max_wallet_debit_usd"`
	FundingAmountUSD       string `json:"funding_amount_usd"`
}

var xPlanKeys = []string{
	"premium_3m", "premium_6m", "premium_12m",
	"premium_plus_3m", "premium_plus_6m", "premium_plus_12m",
}

func migrateXMember() error {
	if DB == nil {
		return nil
	}
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS x_channels (
			channel TEXT PRIMARY KEY,
			account_id INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 0,
			card_id INTEGER NOT NULL DEFAULT 0,
			auto_card INTEGER NOT NULL DEFAULT 0,
			auto_card_product TEXT NOT NULL DEFAULT '',
			auto_card_first_name TEXT NOT NULL DEFAULT '',
			auto_card_last_name TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return err
	}
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS x_plan_limits (
			channel TEXT NOT NULL,
			plan TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			currency TEXT NOT NULL DEFAULT '',
			max_official_amount_minor INTEGER NOT NULL DEFAULT 0,
			max_service_fee_usd TEXT NOT NULL DEFAULT '',
			max_wallet_debit_usd TEXT NOT NULL DEFAULT '',
			funding_amount_usd TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (channel, plan)
		)
	`); err != nil {
		return err
	}
	if err := migrateXCodes(); err != nil {
		return err
	}
	for _, ch := range []string{XChannelCDK, XChannelDirect} {
		if _, err := DB.Exec(`INSERT OR IGNORE INTO x_channels (channel) VALUES (?)`, ch); err != nil {
			return err
		}
		for _, plan := range xPlanKeys {
			if _, err := DB.Exec(`INSERT OR IGNORE INTO x_plan_limits (channel, plan) VALUES (?, ?)`, ch, plan); err != nil {
				return err
			}
		}
	}
	if err := ensureXChannelPayCols(); err != nil {
		return err
	}
	return nil
}

func ensureXChannelPayCols() error {
	specs := []struct{ col, ddl string }{
		{"pay_mode", `ALTER TABLE x_channels ADD COLUMN pay_mode TEXT NOT NULL DEFAULT ''`},
		{"pay_fallback", `ALTER TABLE x_channels ADD COLUMN pay_fallback INTEGER NOT NULL DEFAULT 0`},
		{"card_order", `ALTER TABLE x_channels ADD COLUMN card_order TEXT NOT NULL DEFAULT ''`},
	}
	for _, s := range specs {
		var n int
		if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('x_channels') WHERE name=?`, s.col).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := DB.Exec(s.ddl); err != nil {
			return err
		}
	}
	_, err := DB.Exec(`
		UPDATE x_channels SET pay_mode = CASE
			WHEN auto_card = 1 THEN 'new'
			WHEN card_id > 0 THEN 'fixed'
			ELSE 'existing'
		END WHERE pay_mode = ''
	`)
	return err
}

func ListXChannels() ([]XChannel, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	if err := migrateXMember(); err != nil {
		return nil, err
	}
	rows, err := DB.Query(`
		SELECT channel, account_id, enabled, card_id, auto_card,
		       auto_card_product, auto_card_first_name, auto_card_last_name,
		       COALESCE(pay_mode,''), COALESCE(pay_fallback,0), COALESCE(card_order,''), COALESCE(updated_at,'')
		FROM x_channels ORDER BY channel
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []XChannel
	for rows.Next() {
		var ch XChannel
		var enabled, auto, fallback int
		if err := rows.Scan(&ch.Channel, &ch.AccountID, &enabled, &ch.CardID, &auto,
			&ch.AutoCardProduct, &ch.AutoCardFirstName, &ch.AutoCardLastName,
			&ch.PayMode, &fallback, &ch.CardOrder, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		ch.Enabled = enabled != 0
		ch.AutoCard = auto != 0
		ch.PayFallback = fallback != 0
		ch.CardPrefs = decodeCardPrefs(ch.CardOrder)
		out = append(out, ch)
	}
	return out, rows.Err()
}

func decodeCardPrefs(raw string) []XCardPref {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var prefs []XCardPref
	if err := json.Unmarshal([]byte(raw), &prefs); err != nil {
		return nil
	}
	return prefs
}

func SaveXChannel(ch XChannel) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	if err := ensureXChannelPayCols(); err != nil {
		return err
	}
	ch.Channel = strings.TrimSpace(ch.Channel)
	if ch.Channel != XChannelCDK && ch.Channel != XChannelDirect {
		return fmt.Errorf("未知通道")
	}
	if ch.Enabled {
		if ch.AccountID <= 0 {
			return fmt.Errorf("先绑定卡台再启用通道")
		}
		acc, err := GetCardPlatformAccount(ch.AccountID)
		if err != nil {
			return err
		}
		if acc.Protocol != AccountProtocolAvanfinityAPIv1 {
			return fmt.Errorf("通道只能绑定 X 会员卡台")
		}
		need := CapXCDK
		if ch.Channel == XChannelDirect {
			need = CapXDirect
		}
		if !HasCapability(acc.Capabilities, need) {
			return fmt.Errorf("这个卡台没有勾选对应用途")
		}
		if !strings.EqualFold(acc.Status, "active") {
			return fmt.Errorf("卡台已停用")
		}
	}
	if ch.Channel == XChannelDirect && ch.Enabled && ch.CardID <= 0 {
		return fmt.Errorf("X 直充要选一张付款卡")
	}
	if ch.Channel == XChannelCDK {
		normalizeXPayMode(&ch)
	}
	if ch.Channel == XChannelCDK && ch.Enabled && ch.PayMode == "fixed" && ch.CardID <= 0 {
		return fmt.Errorf("固定付款要指定一张卡")
	}
	if ch.Channel == XChannelCDK && ch.Enabled && ch.PayMode == "new" && !autoCardReady(ch) {
		return fmt.Errorf("自动开卡要选卡种，并填持卡人的名和姓")
	}
	if ch.Channel == XChannelCDK && ch.Enabled && ch.PayMode == "existing" && ch.PayFallback && !autoCardReady(ch) {
		return fmt.Errorf("没有合格卡时要开新卡，先选卡种并填持卡人的名和姓")
	}
	order, err := json.Marshal(ch.CardPrefs)
	if err != nil {
		return err
	}
	if ch.CardPrefs == nil {
		order = []byte(ch.CardOrder)
	}
	_, err = DB.Exec(`
		UPDATE x_channels
		SET account_id = ?, enabled = ?, card_id = ?, auto_card = ?,
		    auto_card_product = ?, auto_card_first_name = ?, auto_card_last_name = ?,
		    pay_mode = ?, pay_fallback = ?, card_order = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE channel = ?
	`, ch.AccountID, boolToInt(ch.Enabled), ch.CardID, boolToInt(ch.AutoCard),
		strings.TrimSpace(ch.AutoCardProduct), strings.TrimSpace(ch.AutoCardFirstName), strings.TrimSpace(ch.AutoCardLastName),
		ch.PayMode, boolToInt(ch.PayFallback), string(order),
		ch.Channel)
	return err
}

func normalizeXPayMode(ch *XChannel) {
	switch strings.TrimSpace(ch.PayMode) {
	case "":
		if ch.AutoCard {
			ch.PayMode = "new"
		} else if ch.CardID > 0 {
			ch.PayMode = "fixed"
		} else {
			ch.PayMode = "existing"
		}
	case "new", "fixed", "existing":
	default:
		ch.PayMode = "existing"
	}
	ch.AutoCard = ch.PayMode == "new"
	if ch.PayMode != "fixed" {
		ch.CardID = 0
	}
}

func autoCardReady(ch XChannel) bool {
	return strings.TrimSpace(ch.AutoCardProduct) != "" &&
		strings.TrimSpace(ch.AutoCardFirstName) != "" &&
		strings.TrimSpace(ch.AutoCardLastName) != ""
}

func ListXPlanLimits(channel string) ([]XPlanLimit, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	if err := migrateXMember(); err != nil {
		return nil, err
	}
	channel = strings.TrimSpace(channel)
	q := `
		SELECT channel, plan, enabled, currency, max_official_amount_minor,
		       max_service_fee_usd, max_wallet_debit_usd, funding_amount_usd
		FROM x_plan_limits`
	var args []any
	if channel != "" {
		q += ` WHERE channel = ?`
		args = append(args, channel)
	}
	q += ` ORDER BY channel, plan`
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []XPlanLimit
	for rows.Next() {
		var p XPlanLimit
		var enabled int
		if err := rows.Scan(&p.Channel, &p.Plan, &enabled, &p.Currency, &p.MaxOfficialAmountMinor,
			&p.MaxServiceFeeUSD, &p.MaxWalletDebitUSD, &p.FundingAmountUSD); err != nil {
			return nil, err
		}
		p.Enabled = enabled != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func SaveXPlanLimits(rows []XPlanLimit) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range rows {
		if p.Channel != XChannelCDK && p.Channel != XChannelDirect {
			return fmt.Errorf("未知通道")
		}
		known := false
		for _, key := range xPlanKeys {
			if p.Plan == key {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("未知套餐 %s", p.Plan)
		}
		if _, err := tx.Exec(`
			UPDATE x_plan_limits
			SET enabled = ?, currency = ?, max_official_amount_minor = ?,
			    max_service_fee_usd = ?, max_wallet_debit_usd = ?, funding_amount_usd = ?,
			    updated_at = CURRENT_TIMESTAMP
			WHERE channel = ? AND plan = ?
		`, boolToInt(p.Enabled), strings.ToLower(strings.TrimSpace(p.Currency)), p.MaxOfficialAmountMinor,
			strings.TrimSpace(p.MaxServiceFeeUSD), strings.TrimSpace(p.MaxWalletDebitUSD), strings.TrimSpace(p.FundingAmountUSD),
			p.Channel, p.Plan); err != nil {
			return err
		}
	}
	return tx.Commit()
}

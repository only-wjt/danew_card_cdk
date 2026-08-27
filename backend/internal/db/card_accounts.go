package db

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

const (
	AccountProtocolSpaceXLegacy     = "spacexcard-legacy"
	AccountProtocolAvanfinity202608 = "avanfinity-2026-08"
)

// CardPlatformAccount 卡台账户（A/B 各一条）。
type CardPlatformAccount struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Protocol         string `json:"protocol"`
	SiteBase         string `json:"site_base"`
	CredPublic       string `json:"cred_public,omitempty"`
	CredSecret       string `json:"-"`
	WebhookSecret    string `json:"-"`
	Status           string `json:"status"`
	Priority         int    `json:"priority"`
	IsPrimaryDefault bool   `json:"is_primary_default"`
	ForceNewCard     bool   `json:"force_new_card"`
	LastOKAt         string `json:"last_ok_at,omitempty"`
	LastError        string `json:"last_error,omitempty"`
	LastErrorAt      string `json:"last_error_at,omitempty"`
	CircuitState     string `json:"circuit_state"`
	CircuitFailCount int    `json:"circuit_fail_count"`
	CircuitOpenedAt  string `json:"circuit_opened_at,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

func migrateCardPlatformAccounts() error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS card_platform_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			protocol TEXT NOT NULL,
			site_base TEXT NOT NULL,
			cred_public TEXT NOT NULL DEFAULT '',
			cred_secret TEXT NOT NULL DEFAULT '',
			webhook_secret TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			priority INTEGER NOT NULL DEFAULT 100,
			is_primary_default INTEGER NOT NULL DEFAULT 0,
			force_new_card INTEGER NOT NULL DEFAULT 0,
			last_ok_at DATETIME,
			last_error TEXT NOT NULL DEFAULT '',
			last_error_at DATETIME,
			circuit_state TEXT NOT NULL DEFAULT 'closed',
			circuit_fail_count INTEGER NOT NULL DEFAULT 0,
			circuit_opened_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cpa_status ON card_platform_accounts(status)`)
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cpa_priority ON card_platform_accounts(priority)`)
	if err := seedDefaultCardPlatformAccount(); err != nil {
		return err
	}
	return ensurePrimaryAccount()
}

func seedDefaultCardPlatformAccount() error {
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM card_platform_accounts`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	base, _ := GetSetting("card_api_base")
	key, _ := GetSetting("card_api_key")
	wh, _ := GetSetting("webhook_secret")
	base = strings.TrimSpace(base)
	key = strings.TrimSpace(key)
	if base == "" && key == "" {
		return nil
	}
	_, err := DB.Exec(`
		INSERT INTO card_platform_accounts
		(name, protocol, site_base, cred_secret, webhook_secret, status, priority, is_primary_default, updated_at)
		VALUES (?, ?, ?, ?, ?, 'active', 10, 1, CURRENT_TIMESTAMP)
	`, "主台 A（现网）", AccountProtocolSpaceXLegacy, base, key, strings.TrimSpace(wh))
	if err != nil {
		log.Printf("seed card_platform_accounts: %v", err)
	}
	return err
}

// ListCardPlatformAccounts 列出全部卡台账户（按 priority 升序）。
func ListCardPlatformAccounts() ([]CardPlatformAccount, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	rows, err := DB.Query(`
		SELECT id, name, protocol, site_base, COALESCE(cred_public,''), COALESCE(cred_secret,''),
		       COALESCE(webhook_secret,''), COALESCE(status,'active'), COALESCE(priority,100),
		       COALESCE(is_primary_default,0), COALESCE(force_new_card,0),
		       COALESCE(last_ok_at,''), COALESCE(last_error,''), COALESCE(last_error_at,''),
		       COALESCE(circuit_state,'closed'), COALESCE(circuit_fail_count,0), COALESCE(circuit_opened_at,''),
		       COALESCE(created_at,''), COALESCE(updated_at,'')
		FROM card_platform_accounts
		ORDER BY priority ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CardPlatformAccount, 0, 4)
	for rows.Next() {
		var a CardPlatformAccount
		var priDef, force int
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Protocol, &a.SiteBase, &a.CredPublic, &a.CredSecret, &a.WebhookSecret,
			&a.Status, &a.Priority, &priDef, &force,
			&a.LastOKAt, &a.LastError, &a.LastErrorAt,
			&a.CircuitState, &a.CircuitFailCount, &a.CircuitOpenedAt,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		a.IsPrimaryDefault = priDef != 0
		a.ForceNewCard = force != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

func GetCardPlatformAccount(id int64) (CardPlatformAccount, error) {
	if id <= 0 {
		return CardPlatformAccount{}, fmt.Errorf("invalid account id")
	}
	all, err := ListCardPlatformAccounts()
	if err != nil {
		return CardPlatformAccount{}, err
	}
	for _, acc := range all {
		if acc.ID == id {
			return acc, nil
		}
	}
	return CardPlatformAccount{}, fmt.Errorf("卡台账户 %d 不存在", id)
}

// ActiveDualIssueAccounts 返回可用于双发的 active 账户（priority 升序，至少 0 条）。
func ActiveDualIssueAccounts() ([]CardPlatformAccount, error) {
	all, err := ListCardPlatformAccounts()
	if err != nil {
		return nil, err
	}
	out := make([]CardPlatformAccount, 0, len(all))
	for _, a := range all {
		if !strings.EqualFold(a.Status, "active") {
			continue
		}
		if !strings.EqualFold(a.CircuitState, "open") {
			out = append(out, a)
		}
	}
	return out, nil
}

func CircuitProbeAccounts() ([]CardPlatformAccount, error) {
	all, err := ListCardPlatformAccounts()
	if err != nil {
		return nil, err
	}
	out := make([]CardPlatformAccount, 0)
	for _, acc := range all {
		if strings.EqualFold(acc.Status, "active") &&
			strings.EqualFold(acc.CircuitState, "open") &&
			circuitProbeDue(acc.CircuitOpenedAt) {
			out = append(out, acc)
		}
	}
	return out, nil
}

func circuitProbeDue(openedAt string) bool {
	openedAt = strings.TrimSpace(openedAt)
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, time.RFC3339Nano} {
		if t, err := time.Parse(layout, openedAt); err == nil {
			return time.Since(t) >= 5*time.Minute
		}
	}
	return false
}

// UpsertCardPlatformAccount 新增或更新卡台账户。凭证留空表示沿用原值，
// 避免管理端为了改个优先级就得重新粘一遍 API Key。
func UpsertCardPlatformAccount(a CardPlatformAccount) (int64, error) {
	if DB == nil {
		return 0, fmt.Errorf("db not ready")
	}
	if strings.TrimSpace(a.Name) == "" {
		return 0, fmt.Errorf("名称必填")
	}
	if strings.TrimSpace(a.SiteBase) == "" {
		return 0, fmt.Errorf("卡台地址必填")
	}
	if strings.TrimSpace(a.Protocol) == "" {
		a.Protocol = AccountProtocolSpaceXLegacy
	}
	if strings.TrimSpace(a.Status) == "" {
		a.Status = "active"
	}
	if a.Priority <= 0 {
		a.Priority = 100
	}
	if a.ID <= 0 && strings.TrimSpace(a.CredSecret) == "" {
		return 0, fmt.Errorf("API Key 必填")
	}
	if a.IsPrimaryDefault && strings.TrimSpace(a.CredSecret) == "" && a.ID > 0 {
		_ = DB.QueryRow(`SELECT cred_secret FROM card_platform_accounts WHERE id = ?`, a.ID).Scan(&a.CredSecret)
	}
	if a.IsPrimaryDefault && strings.TrimSpace(a.CredSecret) == "" {
		return 0, fmt.Errorf("主台必须配置 API Key")
	}
	if a.ID > 0 {
		if _, err := DB.Exec(`
			UPDATE card_platform_accounts
			SET name = ?, protocol = ?, site_base = ?, cred_public = ?,
			    cred_secret = CASE WHEN ? != '' THEN ? ELSE cred_secret END,
			    webhook_secret = CASE WHEN ? != '' THEN ? ELSE webhook_secret END,
			    status = ?, priority = ?, is_primary_default = ?, force_new_card = ?,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, a.Name, a.Protocol, a.SiteBase, a.CredPublic,
			a.CredSecret, a.CredSecret, a.WebhookSecret, a.WebhookSecret,
			a.Status, a.Priority, boolToInt(a.IsPrimaryDefault), boolToInt(a.ForceNewCard), a.ID); err != nil {
			return 0, err
		}
		if a.IsPrimaryDefault {
			if err := clearOtherPrimaryDefaults(a.ID); err != nil {
				return 0, err
			}
			if err := syncPrimaryAccountLegacySettings(a.ID); err != nil {
				return 0, err
			}
		}
		if err := ensurePrimaryAccount(); err != nil {
			return 0, err
		}
		if err := seedPrimaryAccountCardSelection(); err != nil {
			return 0, err
		}
		return a.ID, nil
	}
	res, err := DB.Exec(`
		INSERT INTO card_platform_accounts
		(name, protocol, site_base, cred_public, cred_secret, webhook_secret,
		 status, priority, is_primary_default, force_new_card, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, a.Name, a.Protocol, a.SiteBase, a.CredPublic, a.CredSecret, a.WebhookSecret,
		a.Status, a.Priority, boolToInt(a.IsPrimaryDefault), boolToInt(a.ForceNewCard))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if a.IsPrimaryDefault {
		if err := clearOtherPrimaryDefaults(id); err != nil {
			return id, err
		}
		if err := syncPrimaryAccountLegacySettings(id); err != nil {
			return id, err
		}
	}
	if err := ensurePrimaryAccount(); err != nil {
		return id, err
	}
	if err := seedPrimaryAccountCardSelection(); err != nil {
		return id, err
	}
	return id, nil
}

// SetCardPlatformWebhookSecret 只改某一台的回调验签密钥。
// 空串拒绝：避免「保存空值」把已配 secret 抹掉。
func SetCardPlatformWebhookSecret(id int64, secret string) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	if id <= 0 {
		return fmt.Errorf("invalid account id")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("webhook secret 必填")
	}
	res, err := DB.Exec(`
		UPDATE card_platform_accounts
		SET webhook_secret = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, secret, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("账户不存在")
	}
	var isPrimary int
	if err := DB.QueryRow(`SELECT COALESCE(is_primary_default,0) FROM card_platform_accounts WHERE id = ?`, id).Scan(&isPrimary); err != nil {
		return err
	}
	if isPrimary != 0 {
		return syncPrimaryAccountLegacySettings(id)
	}
	return nil
}

// 旧模块仍从 site_settings 读取主台；管理 UI 只维护账户表，这里自动做兼容镜像，
// 防止出现“顶部一套、下面一套”两个配置源。
func syncPrimaryAccountLegacySettings(id int64) error {
	var base, secret, webhook string
	if err := DB.QueryRow(`
		SELECT site_base, cred_secret, webhook_secret
		FROM card_platform_accounts WHERE id = ?
	`, id).Scan(&base, &secret, &webhook); err != nil {
		return err
	}
	if err := SetSetting("card_api_base", strings.TrimSpace(base)); err != nil {
		return err
	}
	if strings.TrimSpace(secret) != "" {
		if err := SetSetting("card_api_key", strings.TrimSpace(secret)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(webhook) != "" {
		if err := SetSetting("webhook_secret", strings.TrimSpace(webhook)); err != nil {
			return err
		}
	}
	return nil
}

// clearOtherPrimaryDefaults 主台只能有一个，否则老码转发的目标就不确定了。
func clearOtherPrimaryDefaults(keepID int64) error {
	_, err := DB.Exec(`
		UPDATE card_platform_accounts SET is_primary_default = 0, updated_at = CURRENT_TIMESTAMP
		WHERE id != ?
	`, keepID)
	return err
}

func ensurePrimaryAccount() error {
	var id int64
	err := DB.QueryRow(`
		SELECT id FROM card_platform_accounts
		WHERE is_primary_default = 1 AND status = 'active'
		ORDER BY priority, id LIMIT 1
	`).Scan(&id)
	if err != nil {
		err = DB.QueryRow(`
			SELECT id FROM card_platform_accounts
			WHERE status = 'active' ORDER BY priority, id LIMIT 1
		`).Scan(&id)
		if err != nil {
			return nil
		}
		if _, err := DB.Exec(`UPDATE card_platform_accounts SET is_primary_default = CASE WHEN id=? THEN 1 ELSE 0 END`, id); err != nil {
			return err
		}
	}
	if legacyID, _ := GetSetting("legacy_card_platform_account_id"); strings.TrimSpace(legacyID) == "" {
		if err := SetSetting("legacy_card_platform_account_id", strconv.FormatInt(id, 10)); err != nil {
			return err
		}
	}
	return syncPrimaryAccountLegacySettings(id)
}

// LegacyCardPlatformAccount 固定旧码原始归属台；更换主台不会把历史码误发给新台。
func LegacyCardPlatformAccount() (CardPlatformAccount, error) {
	raw, _ := GetSetting("legacy_card_platform_account_id")
	id, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	all, err := ListCardPlatformAccounts()
	if err != nil {
		return CardPlatformAccount{}, err
	}
	for _, acc := range all {
		if acc.ID == id {
			return acc, nil
		}
	}
	acc, err := PrimaryCardPlatformAccount()
	if err == nil && acc.ID > 0 {
		_ = SetSetting("legacy_card_platform_account_id", strconv.FormatInt(acc.ID, 10))
	}
	return acc, err
}

// CardPlatformAccountForLegacyCode 新发原生码记录其发码账户；迁移前老码回落到固定 legacy owner。
func CardPlatformAccountForLegacyCode(code string) (CardPlatformAccount, error) {
	var id int64
	_ = DB.QueryRow(`
		SELECT COALESCE(fulfilled_account_id, 0)
		FROM cardplatform_cdk_codes WHERE code = ? AND code_kind = 'legacy'
	`, strings.TrimSpace(code)).Scan(&id)
	if id > 0 {
		if acc, err := GetCardPlatformAccount(id); err == nil {
			return acc, nil
		}
	}
	return LegacyCardPlatformAccount()
}

// SetCardPlatformAccountStatus 启停一个卡台账户（disabled 后不再参与双发与切台）。
func SetCardPlatformAccountStatus(id int64, status string) error {
	if DB == nil || id <= 0 {
		return fmt.Errorf("invalid account id")
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "active" && status != "disabled" {
		return fmt.Errorf("status must be active or disabled")
	}
	_, err := DB.Exec(`
		UPDATE card_platform_accounts SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, status, id)
	if err != nil {
		return err
	}
	return ensurePrimaryAccount()
}

// ResetCardPlatformCircuit 人工复位熔断（修好卡台后立刻恢复参与）。
func ResetCardPlatformCircuit(id int64) error {
	return MarkCardPlatformAccountOK(id)
}

// CountBindingsByAccount 某卡台上还挂着多少张未使用的绑定（删账户前的安全检查）。
func CountBindingsByAccount(accountID int64) (int, error) {
	if DB == nil {
		return 0, fmt.Errorf("db not ready")
	}
	var n int
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM site_cdk_bindings
		WHERE account_id = ? AND status IN (?, ?, ?)
	`, accountID, BindingStatusUnused, BindingStatusRedeeming, BindingStatusReserved).Scan(&n)
	return n, err
}

// PrimaryCardPlatformAccount 主台：优先 is_primary_default，其次 priority 最小的 active。
// 老码没有 binding，只能按主台转发，所以这里必须有确定答案。
func PrimaryCardPlatformAccount() (CardPlatformAccount, error) {
	all, err := ListCardPlatformAccounts()
	if err != nil {
		return CardPlatformAccount{}, err
	}
	for _, a := range all {
		if a.IsPrimaryDefault {
			return a, nil
		}
	}
	for _, a := range all {
		if strings.EqualFold(a.Status, "active") {
			return a, nil
		}
	}
	return CardPlatformAccount{}, fmt.Errorf("未配置卡台账户")
}

// MarkCardPlatformAccountOK 一次成功调用即复位熔断计数。
func MarkCardPlatformAccountOK(id int64) error {
	if DB == nil || id <= 0 {
		return nil
	}
	_, err := DB.Exec(`
		UPDATE card_platform_accounts
		SET last_ok_at = CURRENT_TIMESTAMP, circuit_state = 'closed', circuit_fail_count = 0,
		    circuit_opened_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, id)
	return err
}

// circuitFailThreshold 连续失败多少次后熔断该台（后续双发/兑换跳过它）。
const circuitFailThreshold = 5

// MarkCardPlatformAccountError 记一次不可达；连续超阈值就打开熔断。
func MarkCardPlatformAccountError(id int64, reason string) error {
	if DB == nil || id <= 0 {
		return nil
	}
	if _, err := DB.Exec(`
		UPDATE card_platform_accounts
		SET last_error = ?, last_error_at = CURRENT_TIMESTAMP,
		    circuit_fail_count = circuit_fail_count + 1,
		    circuit_opened_at = CASE WHEN circuit_state = 'open' THEN CURRENT_TIMESTAMP ELSE circuit_opened_at END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, reason, id); err != nil {
		return err
	}
	_, err := DB.Exec(`
		UPDATE card_platform_accounts
		SET circuit_state = 'open', circuit_opened_at = CURRENT_TIMESTAMP
		WHERE id = ? AND circuit_fail_count >= ? AND circuit_state != 'open'
	`, id, circuitFailThreshold)
	return err
}

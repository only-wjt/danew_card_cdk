package db

import (
	"database/sql"
	"fmt"
	"strings"
)

const (
	CodeKindLegacy     = "legacy"
	CodeKindSite       = "site"
	CodeKindLocalStock = "local_stock"

	BindingStatusUnused    = "unused"
	BindingStatusRedeeming = "redeeming"
	BindingStatusReserved  = "reserved"
	BindingStatusConsumed  = "consumed"
	BindingStatusFailed    = "failed"
	BindingStatusUnknown   = "unknown"
	BindingStatusRetired   = "retired"
	BindingStatusRefunded  = "refunded"
	BindingStatusDisabled  = "disabled"

	IssueStatusPending = "pending"
	IssueStatusActive  = "active"
	IssueStatusFailed  = "failed"
)

// SiteCDKBinding 本站码 ↔ 上游码（1 本站 : N 上游，双绑时 N=2）。
type SiteCDKBinding struct {
	ID               int64  `json:"id"`
	SiteCodeID       int64  `json:"site_code_id"`
	SiteCode         string `json:"site_code"`
	AccountID        int64  `json:"account_id"`
	Provider         string `json:"provider"`
	RemoteID         string `json:"remote_id"`
	RemoteCode       string `json:"-"`
	RemoteCodePrefix string `json:"remote_code_prefix,omitempty"`
	IsPrimary        bool   `json:"is_primary"`
	Status           string `json:"status"`
	LastError        string `json:"last_error,omitempty"`
	IssuedIdemKey    string `json:"-"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

func migrateSiteCDKBindings() error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS site_cdk_bindings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			site_code_id INTEGER NOT NULL,
			site_code TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			provider TEXT NOT NULL,
			remote_id TEXT NOT NULL,
			remote_code TEXT NOT NULL,
			remote_code_prefix TEXT NOT NULL DEFAULT '',
			is_primary INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'unused',
			last_error TEXT NOT NULL DEFAULT '',
			issued_idem_key TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(site_code_id, account_id),
			UNIQUE(account_id, remote_id)
		)
	`)
	if err != nil {
		return err
	}
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_scb_site_code ON site_cdk_bindings(site_code)`)
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_scb_site_code_id ON site_cdk_bindings(site_code_id)`)
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_scb_status ON site_cdk_bindings(status)`)
	return nil
}

// InsertSiteCDKBinding 写入一条上游绑定。
func InsertSiteCDKBinding(b SiteCDKBinding) (int64, error) {
	if DB == nil {
		return 0, fmt.Errorf("db not ready")
	}
	res, err := DB.Exec(`
		INSERT INTO site_cdk_bindings
		(site_code_id, site_code, account_id, provider, remote_id, remote_code, remote_code_prefix,
		 is_primary, status, last_error, issued_idem_key, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, b.SiteCodeID, b.SiteCode, b.AccountID, b.Provider, b.RemoteID, b.RemoteCode, b.RemoteCodePrefix,
		boolToInt(b.IsPrimary), b.Status, b.LastError, b.IssuedIdemKey)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ListBindingsForSiteCode 按本站码查全部 binding（主绑优先）。
func ListBindingsForSiteCode(siteCode string) ([]SiteCDKBinding, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	siteCode = strings.TrimSpace(siteCode)
	rows, err := DB.Query(`
		SELECT id, site_code_id, site_code, account_id, provider, remote_id, remote_code,
		       COALESCE(remote_code_prefix,''), COALESCE(is_primary,0), COALESCE(status,'unused'),
		       COALESCE(last_error,''), COALESCE(issued_idem_key,''),
		       COALESCE(created_at,''), COALESCE(updated_at,'')
		FROM site_cdk_bindings
		WHERE upper(trim(site_code)) = upper(trim(?))
		ORDER BY is_primary DESC, id ASC
	`, siteCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSiteCDKBindings(rows)
}

// ListBindingsForSiteCodeID 按本站行 id 查 binding。
func ListBindingsForSiteCodeID(siteCodeID int64) ([]SiteCDKBinding, error) {
	if DB == nil || siteCodeID <= 0 {
		return nil, fmt.Errorf("invalid site code id")
	}
	rows, err := DB.Query(`
		SELECT id, site_code_id, site_code, account_id, provider, remote_id, remote_code,
		       COALESCE(remote_code_prefix,''), COALESCE(is_primary,0), COALESCE(status,'unused'),
		       COALESCE(last_error,''), COALESCE(issued_idem_key,''),
		       COALESCE(created_at,''), COALESCE(updated_at,'')
		FROM site_cdk_bindings
		WHERE site_code_id = ?
		ORDER BY is_primary DESC, id ASC
	`, siteCodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSiteCDKBindings(rows)
}

func FindSiteBindingByRemote(accountID int64, remoteID string) (SiteCDKBinding, bool) {
	if DB == nil || accountID <= 0 || strings.TrimSpace(remoteID) == "" {
		return SiteCDKBinding{}, false
	}
	var b SiteCDKBinding
	var primary int
	err := DB.QueryRow(`
		SELECT b.id, b.site_code_id, COALESCE(c.code,''), b.account_id, b.provider,
		       COALESCE(b.remote_id,''), COALESCE(b.remote_code,''), COALESCE(b.remote_code_prefix,''),
		       b.is_primary, b.status, COALESCE(b.last_error,''), COALESCE(b.created_at,''), COALESCE(b.updated_at,'')
		FROM site_cdk_bindings b
		JOIN cardplatform_cdk_codes c ON c.id = b.site_code_id
		WHERE b.account_id = ? AND b.remote_id = ?
		ORDER BY b.id DESC LIMIT 1
	`, accountID, strings.TrimSpace(remoteID)).Scan(
		&b.ID, &b.SiteCodeID, &b.SiteCode, &b.AccountID, &b.Provider,
		&b.RemoteID, &b.RemoteCode, &b.RemoteCodePrefix, &primary,
		&b.Status, &b.LastError, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return SiteCDKBinding{}, false
	}
	b.IsPrimary = primary != 0
	return b, true
}

func scanSiteCDKBindings(rows *sql.Rows) ([]SiteCDKBinding, error) {
	out := make([]SiteCDKBinding, 0, 2)
	for rows.Next() {
		var b SiteCDKBinding
		var pri int
		if err := rows.Scan(
			&b.ID, &b.SiteCodeID, &b.SiteCode, &b.AccountID, &b.Provider, &b.RemoteID, &b.RemoteCode,
			&b.RemoteCodePrefix, &pri, &b.Status, &b.LastError, &b.IssuedIdemKey,
			&b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, err
		}
		b.IsPrimary = pri != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpdateBindingStatus 更新 binding 状态。
func UpdateBindingStatus(bindingID int64, status, lastError string) error {
	if DB == nil || bindingID <= 0 {
		return nil
	}
	_, err := DB.Exec(`
		UPDATE site_cdk_bindings SET status = ?, last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, lastError, bindingID)
	return err
}

// MarkBindingReclaimed 双发回滚时把 binding 标成 refunded（已尽力删除/禁用）。
func MarkBindingReclaimed(siteCodeID, accountID int64) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`
		UPDATE site_cdk_bindings SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE site_code_id = ? AND account_id = ?
	`, BindingStatusRefunded, siteCodeID, accountID)
	return err
}

// SetActiveBinding 记录本次兑换选中的 binding，刷新/第二标签都跟这一条。
func SetActiveBinding(siteCodeID, bindingID int64) error {
	if DB == nil || siteCodeID <= 0 {
		return nil
	}
	_, err := DB.Exec(`
		UPDATE cardplatform_cdk_codes SET active_binding_id = ? WHERE id = ?
	`, bindingID, siteCodeID)
	return err
}

// GetActiveBindingID 取本站码已选定的 binding（0 = 尚未选台）。
func GetActiveBindingID(siteCodeID int64) (int64, error) {
	if DB == nil || siteCodeID <= 0 {
		return 0, nil
	}
	var id int64
	err := DB.QueryRow(`
		SELECT COALESCE(active_binding_id, 0) FROM cardplatform_cdk_codes WHERE id = ?
	`, siteCodeID).Scan(&id)
	return id, err
}

// ClaimBindingForRedeem 抢占式把 binding 置为 redeeming。
// 同一本站码并发兑换时只有一个请求能拿到，避免两个台同时被扣。
func ClaimBindingForRedeem(bindingID int64) (bool, error) {
	if DB == nil || bindingID <= 0 {
		return false, fmt.Errorf("invalid binding id")
	}
	res, err := DB.Exec(`
		UPDATE site_cdk_bindings
		SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND (
			status IN (?, ?)
			OR (status = ? AND updated_at <= datetime('now', '-60 seconds'))
		)
	`, BindingStatusRedeeming, bindingID,
		BindingStatusUnused, BindingStatusFailed, BindingStatusRedeeming)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SiblingBindings 同一本站码下除指定 binding 外的其他绑定（核销后要退役它们）。
func SiblingBindings(siteCodeID, exceptBindingID int64) ([]SiteCDKBinding, error) {
	all, err := ListBindingsForSiteCodeID(siteCodeID)
	if err != nil {
		return nil, err
	}
	out := make([]SiteCDKBinding, 0, len(all))
	for _, b := range all {
		if b.ID == exceptBindingID {
			continue
		}
		if b.Status == BindingStatusRefunded || b.Status == BindingStatusRetired ||
			b.Status == BindingStatusConsumed {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

// MarkSiteCDKFulfilled 记录实际履约台与是否切过台。
func MarkSiteCDKFulfilled(siteCodeID, accountID int64, providerName string, failoverUsed bool, failoverReason string) error {
	if DB == nil || siteCodeID <= 0 {
		return nil
	}
	fo := 0
	if failoverUsed {
		fo = 1
	}
	_, err := DB.Exec(`
		UPDATE cardplatform_cdk_codes
		SET fulfilled_account_id = ?, fulfilled_provider = ?, failover_used = ?, failover_reason = ?
		WHERE id = ?
	`, accountID, providerName, fo, failoverReason, siteCodeID)
	return err
}

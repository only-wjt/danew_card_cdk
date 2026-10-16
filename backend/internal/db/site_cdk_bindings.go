package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
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
	// BindingStatusRetiring 兄弟码收回中的中间态：只有一个协程能认领到，
	// 避免多路轮询同时对同一张码发起退款/禁用。路由视同 retired，不再选用。
	BindingStatusRetiring = "retiring"

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
	return migrateBindingRedeemClientID()
}

// migrateBindingRedeemClientID 记录本次兑换用的幂等键。
// 结果不确定时（超时/5xx）binding 停在 redeeming，用户重试必须复用同一个键，
// 否则上游会当成一笔新兑换再扣一次钱。
func migrateBindingRedeemClientID() error {
	if DB == nil {
		return nil
	}
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('site_cdk_bindings') WHERE name='redeem_client_id'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := DB.Exec(`ALTER TABLE site_cdk_bindings ADD COLUMN redeem_client_id TEXT NOT NULL DEFAULT ''`)
	return err
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

// GetSiteBindingByID 按主键取单条 binding（管理端人工解除用）。
func GetSiteBindingByID(bindingID int64) (SiteCDKBinding, bool) {
	if DB == nil || bindingID <= 0 {
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
		WHERE b.id = ?
	`, bindingID).Scan(
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
// 无条件写入：调用方仅在「独占持有该 binding」时使用（见 C8 说明）。
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

// UpdateBindingStatusFrom 条件转换：仅当当前状态在 from 集合内才写入。
// 返回是否真的改到了行，让调用方判断自己是不是「胜出者」。
func UpdateBindingStatusFrom(bindingID int64, from []string, status, lastError string) (bool, error) {
	if DB == nil || bindingID <= 0 || len(from) == 0 {
		return false, nil
	}
	// 绑定顺序必须与 SQL 中 ? 的出现顺序一致：status, lastError, id, from...
	args := make([]any, 0, len(from)+3)
	args = append(args, status, lastError, bindingID)
	ph := make([]string, len(from))
	for i, f := range from {
		ph[i] = "?"
		args = append(args, f)
	}
	res, err := DB.Exec(`
		UPDATE site_cdk_bindings SET status = ?, last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN (`+strings.Join(ph, ",")+`)
	`, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// MarkBindingConsumed 核销成功：条件转入 consumed 并清掉本次的幂等键。
// 返回 true 表示本次是「胜出者」（此前不是 consumed）。
// 键与状态在同一条语句里改，避免出现「已 consumed 但仍挂着旧键」的中间态。
func MarkBindingConsumed(bindingID int64) (bool, error) {
	if DB == nil || bindingID <= 0 {
		return false, nil
	}
	res, err := DB.Exec(`
		UPDATE site_cdk_bindings
		SET status = ?, redeem_client_id = '', last_error = '', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN (?, ?, ?, ?)
	`, BindingStatusConsumed, bindingID,
		BindingStatusRedeeming, BindingStatusUnused, BindingStatusFailed, BindingStatusReserved)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ResetBindingForRetry 业务拒绝后放回可重试，并清掉幂等键：
// 用户会改凭证重来，复用旧键会拿回同一个已被拒的订单。
func ResetBindingForRetry(bindingID int64, lastError string) error {
	if DB == nil || bindingID <= 0 {
		return nil
	}
	_, err := DB.Exec(`
		UPDATE site_cdk_bindings
		SET status = ?, redeem_client_id = '', last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, BindingStatusUnused, lastError, bindingID)
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
// newRedeemClientID 调用方没给幂等键时，由服务端生成一个并落库。
// 不能留空：空键会让下一次重试又生成新键，上游就会当成两笔订单各扣一次。
func newRedeemClientID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return "srv-" + hex.EncodeToString(b)
}

// ClaimBindingForRedeem 认领兑换权。同一 binding 并发只有一个请求能拿到。
//
// candidate 是本次的幂等键。重新认领一个卡在 redeeming 的 binding（上次结果未知
// 或进程中断）时，复用已存的键而不是新键：上游按 clientRequestId 幂等，
// 复用才会拿回原来那笔订单，而不是再开一笔。
// 返回真正应该发给上游的 clientRequestId。
func ClaimBindingForRedeem(bindingID int64, candidate string) (bool, string, error) {
	if DB == nil || bindingID <= 0 {
		return false, "", fmt.Errorf("invalid binding id")
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		// 直接调 API 的调用方可能不带 client_request_id；服务端补一个，
		// 后续重试才能复用它（见 review 发现的空键分支）。
		candidate = newRedeemClientID()
	}
	res, err := DB.Exec(`
		UPDATE site_cdk_bindings
		SET status = ?, updated_at = CURRENT_TIMESTAMP,
		    redeem_client_id = CASE
		        WHEN redeem_client_id = '' OR status IN (?, ?) THEN ?
		        ELSE redeem_client_id END
		WHERE id = ? AND (
			status IN (?, ?)
			OR (status = ? AND updated_at <= datetime('now', '-60 seconds'))
		)
	`, BindingStatusRedeeming,
		BindingStatusUnused, BindingStatusFailed, candidate,
		bindingID,
		BindingStatusUnused, BindingStatusFailed, BindingStatusRedeeming)
	if err != nil {
		return false, "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, "", nil
	}
	var stored string
	if err := DB.QueryRow(`SELECT COALESCE(redeem_client_id,'') FROM site_cdk_bindings WHERE id = ?`, bindingID).Scan(&stored); err != nil {
		return true, candidate, nil
	}
	if strings.TrimSpace(stored) == "" {
		return true, candidate, nil
	}
	return true, stored, nil
}

// BindingRetireLease 收回租约。认领后若进程在写终态前崩溃/被杀，行会停在 retiring；
// 超过租约就允许重新认领，否则这张码会永远从路由里消失（见 review High-2）。
const BindingRetireLease = 10 * time.Minute

// ClaimBindingForRetire 认领「收回这张兄弟码」的权利。
// 从可用态（unused/failed/reserved）条件转入 retiring：并发的多路轮询里只有一个能拿到，
// 其他直接返回 false，从而避免对同一张上游码重复退款/禁用。
// 停在 retiring 超过租约的行允许被重新认领（崩溃恢复）。
func ClaimBindingForRetire(bindingID int64) (bool, error) {
	if DB == nil || bindingID <= 0 {
		return false, nil
	}
	res, err := DB.Exec(`
		UPDATE site_cdk_bindings
		SET status = ?, last_error = '核销后收回中', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND (
			status IN (?, ?, ?)
			OR (status = ? AND updated_at <= datetime('now', ?))
		)
	`, BindingStatusRetiring, bindingID,
		BindingStatusUnused, BindingStatusFailed, BindingStatusReserved,
		BindingStatusRetiring, fmt.Sprintf("-%d seconds", int(BindingRetireLease.Seconds())))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
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
		// 已定论的不再处理：退回成功/已作废/已核销。
		// retiring 必须留着：进程中途崩掉的行会停在 retiring，而「重新认领超期
		// retiring」正是唯一的恢复路径——在这里跳过它，恢复代码就永远跑不到。
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

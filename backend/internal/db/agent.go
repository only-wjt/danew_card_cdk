package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
)

// ErrAgentUsernameTaken 用户名已被占用；调用方据此回 400 而不是把 SQL 约束文案透给前端。
var ErrAgentUsernameTaken = errors.New("agent username already exists")

const (
	// AgentDefaultMaxConcurrent max_concurrent_recharge 的默认值。
	// 该字段计的是「在途明细条数」而非批次数：批量接口一次就能压进几十条，
	// 按批次计数的话闸门等于不存在。
	AgentDefaultMaxConcurrent = 10
	// AgentMaxConcurrentHardCap 单个代理在途条数的硬上限。
	AgentMaxConcurrentHardCap = 200
	// AgentDefaultMaxBatchItems 单批默认条数上限。
	AgentDefaultMaxBatchItems = 20
	// AgentMaxBatchItemsHardCap 单批硬上限，与管理端 batchRechargeMaxItems 对齐。
	AgentMaxBatchItemsHardCap = 100
)

// agentUserCols 所有 scanAgentUser 调用共用的列清单，避免各处 SELECT 漂移。
const agentUserCols = `id, username, display_name, status, allowed_plans, webhook_url, webhook_secret,
	ref_prefix, rate_limit_rpm, max_concurrent_recharge, max_batch_items, created_at, updated_at`

// AgentUser 代理账号（由站长创建，无自助注册）。
type AgentUser struct {
	ID            int64    `json:"id"`
	Username      string   `json:"username"`
	DisplayName   string   `json:"display_name"`
	Status        string   `json:"status"`
	AllowedPlans  []string `json:"allowed_plans"`
	WebhookURL    string   `json:"webhook_url"`
	RefPrefix              string   `json:"ref_prefix"`
	RateLimitRPM           int      `json:"rate_limit_rpm"`
	MaxConcurrentRecharge  int      `json:"max_concurrent_recharge"`
	MaxBatchItems          int      `json:"max_batch_items"`
	CreatedAt              string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
	HasWebhookKey bool     `json:"has_webhook_secret"`
}

// AgentAPIKey 代理 API 密钥元数据（不含明文）。
type AgentAPIKey struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	KeyPrefix  string `json:"key_prefix"`
	Status     string `json:"status"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// AgentRechargeRecord 代理可见的兑换/充值记录（不含敏感凭据）。
type AgentRechargeRecord struct {
	BatchID         string `json:"batch_id"`
	Seq             int    `json:"seq"`
	RequestID       string `json:"request_id"`
	ClientReference string `json:"client_reference,omitempty"`
	Plan            string `json:"plan"`
	CredMode        string `json:"cred_mode"`
	AccountEmail    string `json:"account_email"`
	CDKPrefix       string `json:"cdk_prefix"`
	UpstreamOrderID string `json:"upstream_order_id"`
	Status          string `json:"status"`
	Message         string `json:"message"`
	Source          string `json:"source"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// AgentRecordQuery 代理记录筛选。
type AgentRecordQuery struct {
	AgentUserID int64
	Email       string
	CDK         string
	SessionHash string
	Status      string
	Plan        string
	Page        int
	PageSize    int
}

func migrateAgentPortal() error {
	if DB == nil {
		return nil
	}
	queries := []string{
		`CREATE TABLE IF NOT EXISTS agent_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			display_name TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			allowed_plans TEXT DEFAULT '',
			webhook_url TEXT DEFAULT '',
			webhook_secret TEXT DEFAULT '',
			ref_prefix TEXT DEFAULT '',
			rate_limit_rpm INTEGER DEFAULT 60,
			max_concurrent_recharge INTEGER DEFAULT 10,
			max_batch_items INTEGER DEFAULT 20,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_users_username ON agent_users(username)`,
		`CREATE TABLE IF NOT EXISTS agent_api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			status TEXT DEFAULT 'active',
			last_used_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			revoked_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_api_keys_user ON agent_api_keys(agent_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_api_keys_hash ON agent_api_keys(key_hash)`,
		`CREATE TABLE IF NOT EXISTS agent_webhook_deliveries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_user_id INTEGER NOT NULL,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			batch_id TEXT DEFAULT '',
			request_id TEXT DEFAULT '',
			payload TEXT NOT NULL,
			target_url TEXT DEFAULT '',
			status TEXT DEFAULT 'pending',
			attempts INTEGER DEFAULT 0,
			next_attempt_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_status_code INTEGER DEFAULT 0,
			last_error TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			delivered_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_awd_agent ON agent_webhook_deliveries(agent_user_id, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_awd_due ON agent_webhook_deliveries(status, next_attempt_at)`,
	}
	for _, q := range queries {
		if _, err := DB.Exec(q); err != nil {
			return err
		}
	}
	for _, spec := range []struct {
		table, col, ddl string
	}{
		{"admin_recharge_batches", "agent_user_id", "INTEGER DEFAULT 0"},
		{"admin_recharge_batches", "source", "TEXT DEFAULT 'admin'"},
		{"admin_recharge_items", "session_hash", "TEXT DEFAULT ''"},
		{"admin_recharge_items", "client_reference", "TEXT DEFAULT ''"},
		{"agent_users", "max_concurrent_recharge", "INTEGER DEFAULT 10"},
		{"agent_users", "max_batch_items", fmt.Sprintf("INTEGER DEFAULT %d", AgentDefaultMaxBatchItems)},
	} {
		var n int
		if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, spec.table, spec.col).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := DB.Exec(`ALTER TABLE ` + spec.table + ` ADD COLUMN ` + spec.col + ` ` + spec.ddl); err != nil {
			return err
		}
		// max_batch_items 是随批量充值一起引入的。同一次升级里 max_concurrent_recharge
		// 的含义也从「在途批次数」改成了「在途明细条数」，老库里存的 2 按新语义等于
		// 只允许两条在途，批量接口会直接被卡死，所以把仍是旧默认值的行抬到新默认值。
		if spec.col == "max_batch_items" {
			if _, err := DB.Exec(`UPDATE agent_users SET max_concurrent_recharge = ? WHERE max_concurrent_recharge <= 2`,
				AgentDefaultMaxConcurrent); err != nil {
				return err
			}
		}
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_arb_agent_user ON admin_recharge_batches(agent_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ari_session_hash ON admin_recharge_items(session_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_ari_client_ref ON admin_recharge_items(client_reference)`,
	}
	for _, q := range indexes {
		if _, err := DB.Exec(q); err != nil {
			log.Printf("agent portal index: %v", err)
		}
	}
	return migrateAgentCDKInventory()
}

func encodePlans(plans []string) string {
	if len(plans) == 0 {
		return ""
	}
	b, _ := json.Marshal(plans)
	return string(b)
}

func decodePlans(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

func scanAgentUser(row interface{ Scan(...any) error }) (AgentUser, error) {
	var u AgentUser
	var display, allowed, webhook, webhookSecret, refPrefix sql.NullString
	var rpm, maxConc, maxBatch sql.NullInt64
	err := row.Scan(&u.ID, &u.Username, &display, &u.Status, &allowed, &webhook, &webhookSecret, &refPrefix,
		&rpm, &maxConc, &maxBatch, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return u, err
	}
	u.DisplayName = display.String
	u.AllowedPlans = decodePlans(allowed.String)
	u.WebhookURL = webhook.String
	u.RefPrefix = refPrefix.String
	u.HasWebhookKey = strings.TrimSpace(webhookSecret.String) != ""
	if rpm.Valid && rpm.Int64 > 0 {
		u.RateLimitRPM = int(rpm.Int64)
	} else {
		u.RateLimitRPM = 60
	}
	if maxConc.Valid && maxConc.Int64 > 0 {
		u.MaxConcurrentRecharge = int(maxConc.Int64)
	} else {
		u.MaxConcurrentRecharge = AgentDefaultMaxConcurrent
	}
	if maxBatch.Valid && maxBatch.Int64 > 0 {
		u.MaxBatchItems = int(maxBatch.Int64)
	} else {
		u.MaxBatchItems = AgentDefaultMaxBatchItems
	}
	return u, nil
}

// CreateAgentUser 站长创建代理账号。
func CreateAgentUser(username, password, displayName string, allowedPlans []string) (*AgentUser, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not initialized")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("username required")
	}
	hash, err := HashAdminPassword(password)
	if err != nil {
		return nil, err
	}
	if displayName == "" {
		displayName = username
	}
	res, err := DB.Exec(`
		INSERT INTO agent_users (username, password_hash, display_name, status, allowed_plans, updated_at)
		VALUES (?, ?, ?, 'active', ?, CURRENT_TIMESTAMP)
	`, username, hash, displayName, encodePlans(allowedPlans))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrAgentUsernameTaken
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetAgentUserByID(id)
}

func GetAgentUserByID(id int64) (*AgentUser, error) {
	if DB == nil || id <= 0 {
		return nil, sql.ErrNoRows
	}
	u, err := scanAgentUser(DB.QueryRow(`SELECT `+agentUserCols+` FROM agent_users WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func GetAgentUserByUsername(username string) (*AgentUser, string, error) {
	if DB == nil {
		return nil, "", fmt.Errorf("db not initialized")
	}
	var hash string
	u, err := scanAgentUser(DB.QueryRow(`SELECT `+agentUserCols+` FROM agent_users WHERE username = ?`,
		strings.TrimSpace(username)))
	if err != nil {
		return nil, "", err
	}
	if err := DB.QueryRow(`SELECT password_hash FROM agent_users WHERE id = ?`, u.ID).Scan(&hash); err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

func ListAgentUsers(limit int) ([]AgentUser, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not initialized")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := DB.Query(`SELECT `+agentUserCols+` FROM agent_users ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentUser, 0)
	for rows.Next() {
		u, err := scanAgentUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func UpdateAgentUserStatus(id int64, status string) error {
	if DB == nil {
		return fmt.Errorf("db not initialized")
	}
	_, err := DB.Exec(`UPDATE agent_users SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
	return err
}

func ResetAgentUserPassword(id int64, password string) error {
	hash, err := HashAdminPassword(password)
	if err != nil {
		return err
	}
	_, err = DB.Exec(`UPDATE agent_users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, hash, id)
	return err
}

func UpdateAgentUserPlans(id int64, allowedPlans []string) error {
	_, err := DB.Exec(`UPDATE agent_users SET allowed_plans = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, encodePlans(allowedPlans), id)
	return err
}

func UpdateAgentUserLimits(id int64, rateLimitRPM, maxConcurrent, maxBatchItems int) error {
	if rateLimitRPM <= 0 {
		rateLimitRPM = 60
	}
	if maxConcurrent <= 0 {
		maxConcurrent = AgentDefaultMaxConcurrent
	}
	if maxConcurrent > AgentMaxConcurrentHardCap {
		maxConcurrent = AgentMaxConcurrentHardCap
	}
	if maxBatchItems <= 0 {
		maxBatchItems = AgentDefaultMaxBatchItems
	}
	if maxBatchItems > AgentMaxBatchItemsHardCap {
		maxBatchItems = AgentMaxBatchItemsHardCap
	}
	_, err := DB.Exec(`
		UPDATE agent_users
		SET rate_limit_rpm = ?, max_concurrent_recharge = ?, max_batch_items = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, rateLimitRPM, maxConcurrent, maxBatchItems, id)
	return err
}

// AgentInFlightItemStatuses 未落终态的明细状态集合，与 handler 侧状态机保持一致。
var AgentInFlightItemStatuses = []string{"pending", "issuing", "preparing", "submitted", "processing"}

// CountAgentInFlightRecharges 统计代理当前在途的充值「明细条数」。
//
// 早期版本数的是 status='running' 的批次数，批量接口上线后一批几十条也只记 1，
// 闸门形同虚设，所以改成按明细计数：单条与批量共用同一个额度。
func CountAgentInFlightRecharges(agentID int64) (int, error) {
	if DB == nil || agentID <= 0 {
		return 0, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(AgentInFlightItemStatuses)), ",")
	args := []interface{}{agentID}
	for _, s := range AgentInFlightItemStatuses {
		args = append(args, s)
	}
	var n int
	err := DB.QueryRow(`
		SELECT COUNT(*)
		FROM admin_recharge_items i
		JOIN admin_recharge_batches b ON b.batch_id = i.batch_id
		WHERE b.agent_user_id = ? AND i.status IN (`+ph+`)
	`, args...).Scan(&n)
	return n, err
}

func UpdateAgentUserSettings(id int64, webhookURL, refPrefix string) error {
	_, err := DB.Exec(`
		UPDATE agent_users SET webhook_url = ?, ref_prefix = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, strings.TrimSpace(webhookURL), strings.TrimSpace(refPrefix), id)
	return err
}

func RegenerateAgentWebhookSecret(id int64) (string, error) {
	secret := randomHex(16)
	_, err := DB.Exec(`UPDATE agent_users SET webhook_secret = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, secret, id)
	return secret, err
}

func UpgradeAgentHash(username, newHash string) {
	if newHash == "" {
		return
	}
	_, _ = DB.Exec(`UPDATE agent_users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE username = ?`, newHash, username)
}

func HashAgentAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(plain)))
	return hex.EncodeToString(sum[:])
}

func GenerateAgentAPIKeyPlain() string {
	return "ak_live_" + randomHex(24)
}

func CreateAgentAPIKey(agentUserID int64, name, plainKey string) (*AgentAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}
	prefix := plainKey
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	hash := HashAgentAPIKey(plainKey)
	res, err := DB.Exec(`
		INSERT INTO agent_api_keys (agent_user_id, name, key_prefix, key_hash, status, created_at)
		VALUES (?, ?, ?, ?, 'active', CURRENT_TIMESTAMP)
	`, agentUserID, name, prefix, hash)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetAgentAPIKeyByID(agentUserID, id)
}

func GetAgentAPIKeyByID(agentUserID, id int64) (*AgentAPIKey, error) {
	var k AgentAPIKey
	var lastUsed sql.NullString
	err := DB.QueryRow(`
		SELECT id, name, key_prefix, status, last_used_at, created_at
		FROM agent_api_keys WHERE id = ? AND agent_user_id = ?
	`, id, agentUserID).Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.Status, &lastUsed, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	k.LastUsedAt = lastUsed.String
	return &k, nil
}

func ListAgentAPIKeys(agentUserID int64) ([]AgentAPIKey, error) {
	rows, err := DB.Query(`
		SELECT id, name, key_prefix, status, last_used_at, created_at
		FROM agent_api_keys WHERE agent_user_id = ? AND status != 'revoked' ORDER BY id DESC
	`, agentUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentAPIKey, 0)
	for rows.Next() {
		var k AgentAPIKey
		var lastUsed sql.NullString
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.Status, &lastUsed, &k.CreatedAt); err != nil {
			return nil, err
		}
		k.LastUsedAt = lastUsed.String
		out = append(out, k)
	}
	return out, rows.Err()
}

func RevokeAgentAPIKey(agentUserID, keyID int64) error {
	_, err := DB.Exec(`
		UPDATE agent_api_keys SET status = 'revoked', revoked_at = CURRENT_TIMESTAMP
		WHERE id = ? AND agent_user_id = ?
	`, keyID, agentUserID)
	return err
}

func LookupAgentByAPIKey(plainKey string) (int64, error) {
	hash := HashAgentAPIKey(plainKey)
	var agentID int64
	var status string
	err := DB.QueryRow(`
		SELECT k.agent_user_id, u.status
		FROM agent_api_keys k
		JOIN agent_users u ON u.id = k.agent_user_id
		WHERE k.key_hash = ? AND k.status = 'active'
	`, hash).Scan(&agentID, &status)
	if err != nil {
		return 0, err
	}
	if status != "active" {
		return 0, fmt.Errorf("agent suspended")
	}
	_, _ = DB.Exec(`UPDATE agent_api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE key_hash = ?`, hash)
	return agentID, nil
}

func ListAgentRechargeRecords(q AgentRecordQuery) ([]AgentRechargeRecord, int, error) {
	if DB == nil || q.AgentUserID <= 0 {
		return nil, 0, fmt.Errorf("invalid query")
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}
	offset := (q.Page - 1) * q.PageSize

	where := []string{"b.agent_user_id = ?"}
	args := []interface{}{q.AgentUserID}
	if s := strings.TrimSpace(q.Email); s != "" {
		where = append(where, "i.account_email LIKE ?")
		args = append(args, "%"+s+"%")
	}
	if s := strings.TrimSpace(q.CDK); s != "" {
		where = append(where, "(i.cdk_code LIKE ? OR i.cdk_code LIKE ?)")
		args = append(args, s+"%", "%"+s+"%")
	}
	if s := strings.TrimSpace(q.SessionHash); s != "" {
		where = append(where, "i.session_hash = ?")
		args = append(args, s)
	}
	if s := strings.TrimSpace(q.Status); s != "" {
		where = append(where, "i.status = ?")
		args = append(args, s)
	}
	if s := strings.TrimSpace(q.Plan); s != "" {
		where = append(where, "i.plan = ?")
		args = append(args, s)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	countArgs := append([]interface{}{}, args...)
	if err := DB.QueryRow(`SELECT COUNT(*) FROM admin_recharge_items i JOIN admin_recharge_batches b ON b.batch_id = i.batch_id WHERE `+whereSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, q.PageSize, offset)
	rows, err := DB.Query(`
		SELECT i.batch_id, i.seq, i.client_request_id, COALESCE(i.client_reference,''),
			COALESCE(i.plan,''), COALESCE(i.cred_mode,''), COALESCE(i.account_email,''),
			COALESCE(i.cdk_code,''), COALESCE(i.upstream_order_id,''), COALESCE(i.status,''),
			COALESCE(i.message,''), COALESCE(b.source,''), COALESCE(i.created_at,''), COALESCE(i.updated_at,'')
		FROM admin_recharge_items i
		JOIN admin_recharge_batches b ON b.batch_id = i.batch_id
		WHERE `+whereSQL+`
		ORDER BY i.created_at DESC, i.seq DESC
		LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]AgentRechargeRecord, 0)
	for rows.Next() {
		var r AgentRechargeRecord
		var cdkCode string
		if err := rows.Scan(&r.BatchID, &r.Seq, &r.RequestID, &r.ClientReference, &r.Plan, &r.CredMode,
			&r.AccountEmail, &cdkCode, &r.UpstreamOrderID, &r.Status, &r.Message, &r.Source, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		r.CDKPrefix = cdkDisplayPrefix(cdkCode)
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func GetAgentRechargeItem(agentUserID int64, requestID string) (*AgentRechargeRecord, error) {
	requestID = strings.TrimSpace(requestID)
	var r AgentRechargeRecord
	var cdkCode string
	err := DB.QueryRow(`
		SELECT i.batch_id, i.seq, i.client_request_id, COALESCE(i.client_reference,''),
			COALESCE(i.plan,''), COALESCE(i.cred_mode,''), COALESCE(i.account_email,''),
			COALESCE(i.cdk_code,''), COALESCE(i.upstream_order_id,''), COALESCE(i.status,''),
			COALESCE(i.message,''), COALESCE(b.source,''), COALESCE(i.created_at,''), COALESCE(i.updated_at,'')
		FROM admin_recharge_items i
		JOIN admin_recharge_batches b ON b.batch_id = i.batch_id
		WHERE b.agent_user_id = ? AND i.client_request_id = ?
	`, agentUserID, requestID).Scan(&r.BatchID, &r.Seq, &r.RequestID, &r.ClientReference, &r.Plan, &r.CredMode,
		&r.AccountEmail, &cdkCode, &r.UpstreamOrderID, &r.Status, &r.Message, &r.Source, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.CDKPrefix = cdkDisplayPrefix(cdkCode)
	return &r, nil
}

func cdkDisplayPrefix(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if len(code) <= 16 {
		return code
	}
	parts := strings.Split(code, "-")
	if len(parts) >= 2 {
		return parts[0] + "-" + parts[1]
	}
	return code[:16] + "…"
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", n)
	}
	return hex.EncodeToString(b)
}

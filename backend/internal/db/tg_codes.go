package db

import (
	"database/sql"
	"fmt"
	"strings"
)

const TGChannel = "tg_cdk"

var tgPlanKeys = []string{"premium_3m", "premium_6m", "premium_12m"}

func TGPlanKeys() []string {
	out := make([]string, len(tgPlanKeys))
	copy(out, tgPlanKeys)
	return out
}

func KnownTGPlan(plan string) bool {
	for _, key := range tgPlanKeys {
		if key == plan {
			return true
		}
	}
	return false
}

func migrateTG() error {
	if DB == nil {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tg_plan_limits (
			plan TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 1,
			currency TEXT NOT NULL DEFAULT '',
			max_official_amount_minor INTEGER NOT NULL DEFAULT 0,
			max_wallet_debit_usd TEXT NOT NULL DEFAULT '',
			funding_amount_usd TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS tg_batches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			plan TEXT NOT NULL,
			account_id INTEGER NOT NULL DEFAULT 0,
			quantity INTEGER NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			status TEXT NOT NULL DEFAULT 'pending',
			request_json TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tg_codes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT NOT NULL UNIQUE,
			plan TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'unused',
			upstream_cdk_id TEXT NOT NULL DEFAULT '',
			upstream_code_enc TEXT NOT NULL DEFAULT '',
			upstream_code_prefix TEXT NOT NULL DEFAULT '',
			currency TEXT NOT NULL DEFAULT '',
			max_official_amount_minor INTEGER NOT NULL DEFAULT 0,
			max_wallet_debit_e4 INTEGER NOT NULL DEFAULT 0,
			funding_amount_e4 INTEGER NOT NULL DEFAULT 0,
			pricing_version INTEGER NOT NULL DEFAULT 0,
			device_token TEXT NOT NULL DEFAULT '',
			batch_id INTEGER NOT NULL DEFAULT 0,
			idempotency_key TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			disabled_at DATETIME,
			card_id INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS tg_redemptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tg_code_id INTEGER NOT NULL,
			account_id INTEGER NOT NULL,
			recipient TEXT NOT NULL DEFAULT '',
			client_request_id TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			upstream_status TEXT NOT NULL DEFAULT '',
			amount_minor INTEGER NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT '',
			service_fee_e4 INTEGER NOT NULL DEFAULT 0,
			funding_dispatched INTEGER NOT NULL DEFAULT 0,
			payment_dispatched INTEGER NOT NULL DEFAULT 0,
			payment_attempted INTEGER NOT NULL DEFAULT 0,
			can_retry_preflight INTEGER NOT NULL DEFAULT 0,
			error_code TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			events_json TEXT NOT NULL DEFAULT '[]',
			next_poll_at DATETIME,
			poll_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME,
			resolved_at DATETIME,
			resolved_note TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tg_codes_status ON tg_codes(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tg_redemptions_poll ON tg_redemptions(next_poll_at)`,
	}
	for _, q := range stmts {
		if _, err := DB.Exec(q); err != nil {
			return err
		}
	}
	for _, plan := range tgPlanKeys {
		if _, err := DB.Exec(`INSERT OR IGNORE INTO tg_plan_limits (plan) VALUES (?)`, plan); err != nil {
			return err
		}
	}
	return nil
}

func ensureTG() error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	return migrateTG()
}

type TGPlanLimit struct {
	Plan                   string `json:"plan"`
	Enabled                bool   `json:"enabled"`
	Currency               string `json:"currency"`
	MaxOfficialAmountMinor int64  `json:"max_official_amount_minor"`
	MaxWalletDebitUSD      string `json:"max_wallet_debit_usd"`
	FundingAmountUSD       string `json:"funding_amount_usd"`
}

func ListTGPlanLimits() ([]TGPlanLimit, error) {
	if err := ensureTG(); err != nil {
		return nil, err
	}
	rows, err := DB.Query(`
		SELECT plan, enabled, currency, max_official_amount_minor, max_wallet_debit_usd, funding_amount_usd
		FROM tg_plan_limits ORDER BY plan`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TGPlanLimit
	for rows.Next() {
		var p TGPlanLimit
		var enabled int
		if err := rows.Scan(&p.Plan, &enabled, &p.Currency, &p.MaxOfficialAmountMinor, &p.MaxWalletDebitUSD, &p.FundingAmountUSD); err != nil {
			return nil, err
		}
		p.Enabled = enabled != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func SaveTGPlanLimits(rows []TGPlanLimit) error {
	if err := ensureTG(); err != nil {
		return err
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range rows {
		if !KnownTGPlan(p.Plan) {
			return fmt.Errorf("未知套餐 %s", p.Plan)
		}
		if _, err := tx.Exec(`
			UPDATE tg_plan_limits
			SET enabled = ?, currency = ?, max_official_amount_minor = ?,
			    max_wallet_debit_usd = ?, funding_amount_usd = ?, updated_at = CURRENT_TIMESTAMP
			WHERE plan = ?
		`, boolToInt(p.Enabled), strings.ToLower(strings.TrimSpace(p.Currency)), p.MaxOfficialAmountMinor,
			strings.TrimSpace(p.MaxWalletDebitUSD), strings.TrimSpace(p.FundingAmountUSD), p.Plan); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type TGBatch struct {
	ID             int64
	Plan           string
	AccountID      int64
	Quantity       int
	Note           string
	IdempotencyKey string
	CreatedBy      string
	CreatedAt      string
	Status         string
	RequestJSON    string
}

func InsertTGBatch(b TGBatch) (int64, error) {
	if err := ensureTG(); err != nil {
		return 0, err
	}
	res, err := DB.Exec(`
		INSERT INTO tg_batches (plan, account_id, quantity, note, idempotency_key, created_by, status, request_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.Plan, b.AccountID, b.Quantity, b.Note, b.IdempotencyKey, b.CreatedBy, b.Status, b.RequestJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func SaveTGBatchState(id int64, status, raw string) error {
	_, err := DB.Exec(`UPDATE tg_batches SET status = ?, request_json = ? WHERE id = ?`, status, raw, id)
	return err
}

func SaveTGBatchAttempt(id int64, status, idem, raw string) error {
	_, err := DB.Exec(`UPDATE tg_batches SET status = ?, idempotency_key = ?, request_json = ? WHERE id = ?`, status, idem, raw, id)
	return err
}

func GetTGBatch(id int64) (TGBatch, error) {
	var b TGBatch
	err := DB.QueryRow(`
		SELECT id, plan, account_id, quantity, note, idempotency_key, created_by, COALESCE(created_at,''), status, request_json
		FROM tg_batches WHERE id = ?`, id).Scan(
		&b.ID, &b.Plan, &b.AccountID, &b.Quantity, &b.Note, &b.IdempotencyKey, &b.CreatedBy, &b.CreatedAt, &b.Status, &b.RequestJSON)
	return b, err
}

func ListTGBatches(limit int) ([]map[string]any, error) {
	if err := ensureTG(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 30
	}
	rows, err := DB.Query(`
		SELECT b.id, COALESCE(b.created_at,''), b.plan, b.quantity, b.note, b.status,
		       (SELECT COUNT(*) FROM tg_codes c WHERE c.batch_id = b.id AND c.status NOT IN ('unused','disabled'))
		FROM tg_batches b ORDER BY b.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var at, plan, note, status string
		var qty, used int
		if err := rows.Scan(&id, &at, &plan, &qty, &note, &status, &used); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "created_at": at, "plan": plan, "quantity": qty, "note": note, "status": status, "used": used})
	}
	return out, rows.Err()
}

type TGCode struct {
	ID                     int64
	Code                   string
	Plan                   string
	AccountID              int64
	Status                 string
	UpstreamCDKID          string
	UpstreamCodeEnc        string
	UpstreamCodePrefix     string
	Currency               string
	MaxOfficialAmountMinor int64
	MaxWalletDebitE4       int64
	FundingAmountE4        int64
	PricingVersion         int64
	DeviceToken            string
	BatchID                int64
	IdempotencyKey         string
	Note                   string
	CreatedBy              string
	CreatedAt              string
	DisabledAt             string
	CardID                 int64
}

const tgCodeCols = `id, code, plan, account_id, status, upstream_cdk_id, upstream_code_enc, upstream_code_prefix,
	currency, max_official_amount_minor, max_wallet_debit_e4, funding_amount_e4, pricing_version, device_token,
	batch_id, idempotency_key, note, created_by, COALESCE(created_at,''), COALESCE(disabled_at,''), card_id`

func scanTGCode(row interface{ Scan(...any) error }) (TGCode, error) {
	var c TGCode
	err := row.Scan(&c.ID, &c.Code, &c.Plan, &c.AccountID, &c.Status, &c.UpstreamCDKID, &c.UpstreamCodeEnc, &c.UpstreamCodePrefix,
		&c.Currency, &c.MaxOfficialAmountMinor, &c.MaxWalletDebitE4, &c.FundingAmountE4, &c.PricingVersion, &c.DeviceToken,
		&c.BatchID, &c.IdempotencyKey, &c.Note, &c.CreatedBy, &c.CreatedAt, &c.DisabledAt, &c.CardID)
	return c, err
}

func InsertTGCode(c TGCode) (int64, error) {
	res, err := DB.Exec(`
		INSERT INTO tg_codes (
			code, plan, account_id, status, upstream_cdk_id, upstream_code_enc, upstream_code_prefix,
			currency, max_official_amount_minor, max_wallet_debit_e4, funding_amount_e4, pricing_version,
			device_token, batch_id, idempotency_key, note, created_by, card_id
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.Code, c.Plan, c.AccountID, c.Status, c.UpstreamCDKID, c.UpstreamCodeEnc, c.UpstreamCodePrefix,
		c.Currency, c.MaxOfficialAmountMinor, c.MaxWalletDebitE4, c.FundingAmountE4, c.PricingVersion,
		c.DeviceToken, c.BatchID, c.IdempotencyKey, c.Note, c.CreatedBy, c.CardID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetTGCodeByCode(code string) (TGCode, error) {
	if err := ensureTG(); err != nil {
		return TGCode{}, err
	}
	row := DB.QueryRow(`SELECT `+tgCodeCols+` FROM tg_codes WHERE code = ?`, strings.TrimSpace(code))
	c, err := scanTGCode(row)
	if err == sql.ErrNoRows {
		return TGCode{}, fmt.Errorf("卡密不存在")
	}
	return c, err
}

func GetTGCode(id int64) (TGCode, error) {
	row := DB.QueryRow(`SELECT `+tgCodeCols+` FROM tg_codes WHERE id = ?`, id)
	return scanTGCode(row)
}

func UpdateTGCodeStatus(id int64, status string) error {
	disabled := any(nil)
	if status == "disabled" {
		disabled = "now"
	}
	if status == "disabled" {
		_, err := DB.Exec(`UPDATE tg_codes SET status = ?, disabled_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
		return err
	}
	_ = disabled
	_, err := DB.Exec(`UPDATE tg_codes SET status = ? WHERE id = ?`, status, id)
	return err
}

func HasTGUpstreamCDK(id string) (bool, error) {
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM tg_codes WHERE upstream_cdk_id = ?`, id).Scan(&n)
	return n > 0, err
}

func ListTGCodesByBatch(batchID int64) ([]TGCode, error) {
	rows, err := DB.Query(`SELECT `+tgCodeCols+` FROM tg_codes WHERE batch_id = ? ORDER BY id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TGCode
	for rows.Next() {
		c, err := scanTGCode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type TGRedemption struct {
	ID                int64
	TGCodeID          int64
	AccountID         int64
	Recipient         string
	ClientRequestID   string
	IdempotencyKey    string
	UpstreamStatus    string
	AmountMinor       int64
	Currency          string
	ServiceFeeE4      int64
	FundingDispatched bool
	PaymentDispatched bool
	PaymentAttempted  bool
	CanRetryPreflight bool
	ErrorCode         string
	Message           string
	EventsJSON        string
	NextPollAt        string
	PollCount         int
	CreatedAt         string
	UpdatedAt         string
	FinishedAt        string
	ResolvedAt        string
	ResolvedNote      string
}

func InsertTGRedemption(r TGRedemption) (int64, error) {
	res, err := DB.Exec(`
		INSERT INTO tg_redemptions (
			tg_code_id, account_id, recipient, client_request_id, idempotency_key, upstream_status,
			amount_minor, currency, service_fee_e4, funding_dispatched, payment_dispatched, payment_attempted,
			can_retry_preflight, error_code, message, events_json, next_poll_at, poll_count, finished_at, resolved_note
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.TGCodeID, r.AccountID, r.Recipient, r.ClientRequestID, r.IdempotencyKey, r.UpstreamStatus,
		r.AmountMinor, r.Currency, r.ServiceFeeE4, boolToInt(r.FundingDispatched), boolToInt(r.PaymentDispatched), boolToInt(r.PaymentAttempted),
		boolToInt(r.CanRetryPreflight), r.ErrorCode, r.Message, emptyJSON(r.EventsJSON), nullTime(r.NextPollAt), r.PollCount, nullTime(r.FinishedAt), r.ResolvedNote)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func emptyJSON(s string) string {
	if strings.TrimSpace(s) == "" {
		return "[]"
	}
	return s
}

func nullTime(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func LatestTGRedemption(codeID int64) (TGRedemption, error) {
	var r TGRedemption
	var fund, pay, att, retry int
	err := DB.QueryRow(`
		SELECT id, tg_code_id, account_id, recipient, client_request_id, idempotency_key, upstream_status,
		       amount_minor, currency, service_fee_e4, funding_dispatched, payment_dispatched, payment_attempted,
		       can_retry_preflight, error_code, message, events_json, COALESCE(next_poll_at,''), poll_count,
		       COALESCE(created_at,''), COALESCE(updated_at,''), COALESCE(finished_at,''), COALESCE(resolved_at,''), resolved_note
		FROM tg_redemptions WHERE tg_code_id = ? ORDER BY id DESC LIMIT 1`, codeID).Scan(
		&r.ID, &r.TGCodeID, &r.AccountID, &r.Recipient, &r.ClientRequestID, &r.IdempotencyKey, &r.UpstreamStatus,
		&r.AmountMinor, &r.Currency, &r.ServiceFeeE4, &fund, &pay, &att,
		&retry, &r.ErrorCode, &r.Message, &r.EventsJSON, &r.NextPollAt, &r.PollCount,
		&r.CreatedAt, &r.UpdatedAt, &r.FinishedAt, &r.ResolvedAt, &r.ResolvedNote)
	r.FundingDispatched = fund != 0
	r.PaymentDispatched = pay != 0
	r.PaymentAttempted = att != 0
	r.CanRetryPreflight = retry != 0
	return r, err
}

func GetTGRedemption(id int64) (TGRedemption, error) {
	var r TGRedemption
	var fund, pay, att, retry int
	err := DB.QueryRow(`
		SELECT id, tg_code_id, account_id, recipient, client_request_id, idempotency_key, upstream_status,
		       amount_minor, currency, service_fee_e4, funding_dispatched, payment_dispatched, payment_attempted,
		       can_retry_preflight, error_code, message, events_json, COALESCE(next_poll_at,''), poll_count,
		       COALESCE(created_at,''), COALESCE(updated_at,''), COALESCE(finished_at,''), COALESCE(resolved_at,''), resolved_note
		FROM tg_redemptions WHERE id = ?`, id).Scan(
		&r.ID, &r.TGCodeID, &r.AccountID, &r.Recipient, &r.ClientRequestID, &r.IdempotencyKey, &r.UpstreamStatus,
		&r.AmountMinor, &r.Currency, &r.ServiceFeeE4, &fund, &pay, &att,
		&retry, &r.ErrorCode, &r.Message, &r.EventsJSON, &r.NextPollAt, &r.PollCount,
		&r.CreatedAt, &r.UpdatedAt, &r.FinishedAt, &r.ResolvedAt, &r.ResolvedNote)
	r.FundingDispatched = fund != 0
	r.PaymentDispatched = pay != 0
	r.PaymentAttempted = att != 0
	r.CanRetryPreflight = retry != 0
	return r, err
}

func SaveTGRedemption(r TGRedemption) error {
	return saveTGRedemption(DB, r)
}

func saveTGRedemption(exec interface {
	Exec(string, ...any) (sql.Result, error)
}, r TGRedemption) error {
	_, err := exec.Exec(`
		UPDATE tg_redemptions SET
			recipient = ?, upstream_status = ?, amount_minor = ?, currency = ?, service_fee_e4 = ?,
			funding_dispatched = ?, payment_dispatched = ?, payment_attempted = ?, can_retry_preflight = ?,
			error_code = ?, message = ?, events_json = ?, next_poll_at = ?, poll_count = ?,
			finished_at = ?, resolved_at = ?, resolved_note = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		r.Recipient, r.UpstreamStatus, r.AmountMinor, r.Currency, r.ServiceFeeE4,
		boolToInt(r.FundingDispatched), boolToInt(r.PaymentDispatched), boolToInt(r.PaymentAttempted), boolToInt(r.CanRetryPreflight),
		r.ErrorCode, r.Message, emptyJSON(r.EventsJSON), nullTime(r.NextPollAt), r.PollCount,
		nullTime(r.FinishedAt), nullTime(r.ResolvedAt), r.ResolvedNote, r.ID)
	return err
}

func DueTGRedemptions(limit int) ([]TGRedemption, error) {
	if err := ensureTG(); err != nil {
		return nil, err
	}
	rows, err := DB.Query(`
		SELECT id FROM tg_redemptions
		WHERE next_poll_at IS NOT NULL AND next_poll_at <= CURRENT_TIMESTAMP AND COALESCE(finished_at,'') = ''
		ORDER BY next_poll_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TGRedemption
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		r, err := GetTGRedemption(id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func tgGroupSQL() string {
	return `CASE
		WHEN COALESCE(r.resolved_at,'') != '' AND c.status != 'completed' THEN 'done'
		WHEN c.status = 'unused' THEN 'unused'
		WHEN c.status = 'completed' THEN 'done'
		WHEN c.status = 'disabled' THEN 'failed'
		WHEN c.status IN ('uncertain','review_required','requires_action') THEN 'todo'
		WHEN COALESCE(r.error_code,'') = 'SPENDABLE_BALANCE_INSUFFICIENT' THEN 'todo'
		ELSE 'running'
	END`
}

func ListTGRecords(group, q, plan string, limit int) ([]map[string]any, int, error) {
	if err := ensureTG(); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	where, args := tgFilter(group, q, plan)
	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM tg_codes c LEFT JOIN tg_redemptions r ON r.id = (
		SELECT id FROM tg_redemptions WHERE tg_code_id = c.id ORDER BY id DESC LIMIT 1)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qargs := append(append([]any{}, args...), limit)
	rows, err := DB.Query(`
		SELECT c.id, c.code, c.plan, c.status, c.note, COALESCE(c.created_at,''),
		       COALESCE(r.id,0), COALESCE(r.recipient,''), COALESCE(r.amount_minor,0), COALESCE(r.currency,c.currency),
		       COALESCE(r.service_fee_e4,0), COALESCE(r.message,''), COALESCE(r.events_json,'[]'),
		       COALESCE(r.upstream_status,''), COALESCE(r.poll_count,0), COALESCE(r.error_code,''),
		       `+tgGroupSQL()+`
		FROM tg_codes c
		LEFT JOIN tg_redemptions r ON r.id = (SELECT id FROM tg_redemptions WHERE tg_code_id = c.id ORDER BY id DESC LIMIT 1)
		`+where+` ORDER BY c.id DESC LIMIT ?`, qargs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, redID, amount, fee int64
		var code, plan, status, note, at, recipient, currency, message, events, upstream, errCode, group string
		var polls int
		if err := rows.Scan(&id, &code, &plan, &status, &note, &at, &redID, &recipient, &amount, &currency, &fee, &message, &events, &upstream, &polls, &errCode, &group); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{
			"code_id": id, "code": code, "plan": plan, "status": status, "group": group,
			"note": note, "created_at": at, "redemption_id": redID, "recipient": recipient,
			"amount_minor": amount, "currency": currency, "service_fee_e4": fee,
			"message": message, "events_json": events, "upstream_status": upstream,
			"poll_count": polls, "error_code": errCode,
		})
	}
	return out, total, rows.Err()
}

func tgFilter(group, q, plan string) (string, []any) {
	var conds []string
	var args []any
	if g := strings.TrimSpace(group); g != "" && g != "all" {
		conds = append(conds, tgGroupSQL()+" = ?")
		args = append(args, g)
	}
	if p := strings.TrimSpace(plan); p != "" {
		conds = append(conds, "c.plan = ?")
		args = append(args, p)
	}
	if q = strings.TrimSpace(q); q != "" {
		like := "%" + q + "%"
		conds = append(conds, `(c.code LIKE ? COLLATE NOCASE OR COALESCE(r.recipient,'') LIKE ? COLLATE NOCASE OR COALESCE(c.note,'') LIKE ? COLLATE NOCASE)`)
		args = append(args, like, like, like)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func TGOverview() (map[string]any, error) {
	if err := ensureTG(); err != nil {
		return nil, err
	}
	var unused, running, todo, done int
	_ = DB.QueryRow(`SELECT COUNT(*) FROM tg_codes WHERE status = 'unused'`).Scan(&unused)
	_ = DB.QueryRow(`SELECT COUNT(*) FROM tg_codes WHERE status IN ('quoted','funding','funded','paying','paid_pending_delivery')`).Scan(&running)
	_ = DB.QueryRow(`SELECT COUNT(*) FROM tg_codes WHERE status IN ('uncertain','review_required','requires_action')`).Scan(&todo)
	_ = DB.QueryRow(`SELECT COUNT(*) FROM tg_codes WHERE status = 'completed' AND created_at >= date('now')`).Scan(&done)
	return map[string]any{"unused": unused, "running": running, "todo": todo, "done_today": done}, nil
}

// CompleteTGRedemption atomically persists completion and its notification.
// Persisted code status gates new events; historical successes are not replayed.
// Redemption IDs identify attempts independently of upstream account/code names.
func CompleteTGRedemption(r TGRedemption, text string) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT c.status FROM tg_codes c
		JOIN tg_redemptions r ON r.tg_code_id = c.id AND r.account_id = c.account_id
		WHERE c.id = ? AND r.id = ? AND c.account_id = ?`, r.TGCodeID, r.ID, r.AccountID).Scan(&status); err != nil {
		return err
	}
	if status == "completed" {
		// Keep requery/manual-resolution metadata updates compatible, without
		// manufacturing a notification for an already-completed code.
		if err := saveTGRedemption(tx, r); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.Exec(`UPDATE tg_codes SET status = 'completed' WHERE id = ?`, r.TGCodeID); err != nil {
		return err
	}
	if err := saveTGRedemption(tx, r); err != nil {
		return err
	}
	if err := EnqueueTelegramNotificationTx(tx, fmt.Sprintf("tg:redemption:%d", r.ID), text); err != nil {
		return err
	}
	return tx.Commit()
}

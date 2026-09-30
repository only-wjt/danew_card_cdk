package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// XCode 是一张本站 DNX- 码。上游完整码只以密文存在，不进 API。
type XCode struct {
	ID                     int64
	Code                   string
	Plan                   string
	Channel                string
	AccountID              int64
	Status                 string
	UpstreamCDKID          string
	UpstreamCodeEnc        string
	UpstreamCodePrefix     string
	Currency               string
	MaxOfficialAmountMinor int64
	MaxWalletDebitE4       int64
	FundingAmountE4        int64
	ServiceFeeE4           int64
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

// XRedemption 是一次兑换尝试。调用上游前必须已经落库。
type XRedemption struct {
	ID                int64
	XCodeID           int64
	Channel           string
	AccountID         int64
	Recipient         string
	ClientRequestID   string
	IdempotencyKey    string
	UpstreamOrderID   string
	UpstreamStatus    string
	AmountMinor       int64
	Currency          string
	EstimatedUSDE4    int64
	ServiceFeeE4      int64
	PricingVersion    int64
	PaymentAttempted  bool
	FundingDispatched bool
	PaymentDispatched bool
	CanRetryPreflight bool
	InvoiceURLsEnc    string
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

type XBatch struct {
	ID             int64
	Plan           string
	Channel        string
	AccountID      int64
	Quantity       int
	Note           string
	IdempotencyKey string
	CreatedBy      string
	CreatedAt      string
	Status         string
	RequestJSON    string
}

func migrateXCodes() error {
	if DB == nil {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS x_batches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			plan TEXT NOT NULL,
			channel TEXT NOT NULL,
			account_id INTEGER NOT NULL DEFAULT 0,
			quantity INTEGER NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			status TEXT NOT NULL DEFAULT 'issued',
			request_json TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS x_codes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT NOT NULL UNIQUE,
			plan TEXT NOT NULL,
			channel TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'unused',
			upstream_cdk_id TEXT NOT NULL DEFAULT '',
			upstream_code_enc TEXT NOT NULL DEFAULT '',
			upstream_code_prefix TEXT NOT NULL DEFAULT '',
			currency TEXT NOT NULL DEFAULT '',
			max_official_amount_minor INTEGER NOT NULL DEFAULT 0,
			max_wallet_debit_e4 INTEGER NOT NULL DEFAULT 0,
			funding_amount_e4 INTEGER NOT NULL DEFAULT 0,
			service_fee_e4 INTEGER NOT NULL DEFAULT 0,
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
		`CREATE TABLE IF NOT EXISTS x_redemptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			x_code_id INTEGER NOT NULL,
			channel TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			recipient TEXT NOT NULL DEFAULT '',
			client_request_id TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			upstream_order_id TEXT NOT NULL DEFAULT '',
			upstream_status TEXT NOT NULL DEFAULT '',
			amount_minor INTEGER NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT '',
			estimated_usd_e4 INTEGER NOT NULL DEFAULT 0,
			service_fee_e4 INTEGER NOT NULL DEFAULT 0,
			pricing_version INTEGER NOT NULL DEFAULT 0,
			payment_attempted INTEGER NOT NULL DEFAULT 0,
			funding_dispatched INTEGER NOT NULL DEFAULT 0,
			payment_dispatched INTEGER NOT NULL DEFAULT 0,
			can_retry_preflight INTEGER NOT NULL DEFAULT 0,
			invoice_urls_enc TEXT NOT NULL DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS x_quote_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel TEXT NOT NULL,
			plan TEXT NOT NULL,
			currency TEXT NOT NULL DEFAULT '',
			amount_minor INTEGER NOT NULL DEFAULT 0,
			estimated_usd_e4 INTEGER NOT NULL DEFAULT 0,
			service_fee_e4 INTEGER NOT NULL DEFAULT 0,
			pricing_version INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS x_upstream_calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id INTEGER NOT NULL DEFAULT 0,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			status INTEGER NOT NULL DEFAULT 0,
			detail TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_x_codes_status ON x_codes(status)`,
		`CREATE INDEX IF NOT EXISTS idx_x_redemptions_poll ON x_redemptions(next_poll_at)`,
	}
	for _, q := range stmts {
		if _, err := DB.Exec(q); err != nil {
			return err
		}
	}
	return ensureXExtraCols()
}

func ensureXExtraCols() error {
	if DB == nil {
		return nil
	}
	specs := []struct{ table, col, ddl string }{
		{"x_codes", "card_id", `ALTER TABLE x_codes ADD COLUMN card_id INTEGER NOT NULL DEFAULT 0`},
		{"x_batches", "status", `ALTER TABLE x_batches ADD COLUMN status TEXT NOT NULL DEFAULT 'issued'`},
		{"x_batches", "request_json", `ALTER TABLE x_batches ADD COLUMN request_json TEXT NOT NULL DEFAULT ''`},
	}
	for _, s := range specs {
		var n int
		q := `SELECT COUNT(*) FROM pragma_table_info('` + s.table + `') WHERE name=?`
		if err := DB.QueryRow(q, s.col).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := DB.Exec(s.ddl); err != nil {
			return err
		}
	}
	return nil
}

func InsertXBatch(b XBatch) (int64, error) {
	if b.Status == "" {
		b.Status = "issued"
	}
	res, err := DB.Exec(`
		INSERT INTO x_batches (plan, channel, account_id, quantity, note, idempotency_key, created_by, status, request_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, b.Plan, b.Channel, b.AccountID, b.Quantity, b.Note, b.IdempotencyKey, b.CreatedBy, b.Status, b.RequestJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func InsertXCode(c XCode) (int64, error) {
	res, err := DB.Exec(`
		INSERT INTO x_codes (
			code, plan, channel, account_id, status, upstream_cdk_id, upstream_code_enc, upstream_code_prefix,
			currency, max_official_amount_minor, max_wallet_debit_e4, funding_amount_e4, service_fee_e4,
			pricing_version, device_token, batch_id, idempotency_key, note, created_by, card_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, c.Code, c.Plan, c.Channel, c.AccountID, c.Status, c.UpstreamCDKID, c.UpstreamCodeEnc, c.UpstreamCodePrefix,
		c.Currency, c.MaxOfficialAmountMinor, c.MaxWalletDebitE4, c.FundingAmountE4, c.ServiceFeeE4,
		c.PricingVersion, c.DeviceToken, c.BatchID, c.IdempotencyKey, c.Note, c.CreatedBy, c.CardID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetXCodeByCode(code string) (XCode, error) {
	return scanXCode(`SELECT `+xCodeCols+` FROM x_codes WHERE code = ?`, strings.TrimSpace(code))
}

func GetXCode(id int64) (XCode, error) {
	return scanXCode(`SELECT `+xCodeCols+` FROM x_codes WHERE id = ?`, id)
}

const xCodeCols = `id, code, plan, channel, account_id, status, upstream_cdk_id, upstream_code_enc, upstream_code_prefix,
	currency, max_official_amount_minor, max_wallet_debit_e4, funding_amount_e4, service_fee_e4, pricing_version,
	device_token, batch_id, idempotency_key, note, created_by, COALESCE(created_at,''), COALESCE(disabled_at,''), card_id`

func scanXCode(q string, arg any) (XCode, error) {
	var c XCode
	err := DB.QueryRow(q, arg).Scan(
		&c.ID, &c.Code, &c.Plan, &c.Channel, &c.AccountID, &c.Status, &c.UpstreamCDKID, &c.UpstreamCodeEnc, &c.UpstreamCodePrefix,
		&c.Currency, &c.MaxOfficialAmountMinor, &c.MaxWalletDebitE4, &c.FundingAmountE4, &c.ServiceFeeE4, &c.PricingVersion,
		&c.DeviceToken, &c.BatchID, &c.IdempotencyKey, &c.Note, &c.CreatedBy, &c.CreatedAt, &c.DisabledAt, &c.CardID,
	)
	if err == sql.ErrNoRows {
		return XCode{}, fmt.Errorf("卡密不存在")
	}
	return c, err
}

func UpdateXCodeStatus(id int64, status string) error {
	disabled := any(nil)
	if status == "disabled" {
		disabled = time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	_, err := DB.Exec(`UPDATE x_codes SET status = ?, disabled_at = COALESCE(?, disabled_at) WHERE id = ?`, status, disabled, id)
	return err
}

func InsertXRedemption(r XRedemption) (int64, error) {
	res, err := DB.Exec(`
		INSERT INTO x_redemptions (
			x_code_id, channel, account_id, recipient, client_request_id, idempotency_key, events_json, next_poll_at
		) VALUES (?, ?, ?, ?, ?, ?, '[]', ?)
	`, r.XCodeID, r.Channel, r.AccountID, r.Recipient, r.ClientRequestID, r.IdempotencyKey, r.NextPollAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func LatestXRedemption(codeID int64) (XRedemption, error) {
	return scanRedemption(`SELECT `+xRedCols+` FROM x_redemptions WHERE x_code_id = ? ORDER BY id DESC LIMIT 1`, codeID)
}

func GetXRedemption(id int64) (XRedemption, error) {
	red, err := scanRedemption(`SELECT `+xRedCols+` FROM x_redemptions WHERE id = ?`, id)
	if err != nil {
		return red, fmt.Errorf("兑换记录不存在")
	}
	return red, nil
}

const xRedCols = `id, x_code_id, channel, account_id, recipient, client_request_id, idempotency_key, upstream_order_id,
	upstream_status, amount_minor, currency, estimated_usd_e4, service_fee_e4, pricing_version,
	payment_attempted, funding_dispatched, payment_dispatched, can_retry_preflight, invoice_urls_enc,
	error_code, message, events_json, COALESCE(next_poll_at,''), poll_count,
	COALESCE(created_at,''), COALESCE(updated_at,''), COALESCE(finished_at,''), COALESCE(resolved_at,''), resolved_note`

func scanRedemption(q string, arg any) (XRedemption, error) {
	var r XRedemption
	var pay, fund, dispatched, retry int
	err := DB.QueryRow(q, arg).Scan(
		&r.ID, &r.XCodeID, &r.Channel, &r.AccountID, &r.Recipient, &r.ClientRequestID, &r.IdempotencyKey, &r.UpstreamOrderID,
		&r.UpstreamStatus, &r.AmountMinor, &r.Currency, &r.EstimatedUSDE4, &r.ServiceFeeE4, &r.PricingVersion,
		&pay, &fund, &dispatched, &retry, &r.InvoiceURLsEnc,
		&r.ErrorCode, &r.Message, &r.EventsJSON, &r.NextPollAt, &r.PollCount,
		&r.CreatedAt, &r.UpdatedAt, &r.FinishedAt, &r.ResolvedAt, &r.ResolvedNote,
	)
	if err == sql.ErrNoRows {
		return XRedemption{}, sql.ErrNoRows
	}
	r.PaymentAttempted = pay != 0
	r.FundingDispatched = fund != 0
	r.PaymentDispatched = dispatched != 0
	r.CanRetryPreflight = retry != 0
	return r, err
}

func SaveXRedemption(r XRedemption) error {
	_, err := DB.Exec(`
		UPDATE x_redemptions SET
			recipient = ?, upstream_order_id = ?, upstream_status = ?, amount_minor = ?, currency = ?,
			estimated_usd_e4 = ?, service_fee_e4 = ?, pricing_version = ?,
			payment_attempted = ?, funding_dispatched = ?, payment_dispatched = ?, can_retry_preflight = ?,
			invoice_urls_enc = ?, error_code = ?, message = ?, events_json = ?, next_poll_at = ?, poll_count = ?,
			updated_at = CURRENT_TIMESTAMP, finished_at = ?, resolved_at = ?, resolved_note = ?
		WHERE id = ?
	`, r.Recipient, r.UpstreamOrderID, r.UpstreamStatus, r.AmountMinor, r.Currency,
		r.EstimatedUSDE4, r.ServiceFeeE4, r.PricingVersion,
		boolToInt(r.PaymentAttempted), boolToInt(r.FundingDispatched), boolToInt(r.PaymentDispatched), boolToInt(r.CanRetryPreflight),
		r.InvoiceURLsEnc, r.ErrorCode, r.Message, r.EventsJSON, nullIfEmpty(r.NextPollAt), r.PollCount,
		nullIfEmpty(r.FinishedAt), nullIfEmpty(r.ResolvedAt), r.ResolvedNote, r.ID)
	return err
}

func DueXRedemptions(limit int) ([]XRedemption, error) {
	rows, err := DB.Query(`
		SELECT `+xRedCols+` FROM x_redemptions
		WHERE finished_at IS NULL AND next_poll_at IS NOT NULL AND next_poll_at <= CURRENT_TIMESTAMP
		ORDER BY next_poll_at LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []XRedemption
	for rows.Next() {
		var r XRedemption
		var pay, fund, dispatched, retry int
		if err := rows.Scan(
			&r.ID, &r.XCodeID, &r.Channel, &r.AccountID, &r.Recipient, &r.ClientRequestID, &r.IdempotencyKey, &r.UpstreamOrderID,
			&r.UpstreamStatus, &r.AmountMinor, &r.Currency, &r.EstimatedUSDE4, &r.ServiceFeeE4, &r.PricingVersion,
			&pay, &fund, &dispatched, &retry, &r.InvoiceURLsEnc,
			&r.ErrorCode, &r.Message, &r.EventsJSON, &r.NextPollAt, &r.PollCount,
			&r.CreatedAt, &r.UpdatedAt, &r.FinishedAt, &r.ResolvedAt, &r.ResolvedNote,
		); err != nil {
			return nil, err
		}
		r.PaymentAttempted = pay != 0
		r.FundingDispatched = fund != 0
		r.PaymentDispatched = dispatched != 0
		r.CanRetryPreflight = retry != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func InsertQuoteSample(channel, plan, currency, source string, amountMinor, estimatedE4, feeE4, version int64) error {
	_, err := DB.Exec(`
		INSERT INTO x_quote_samples (channel, plan, currency, amount_minor, estimated_usd_e4, service_fee_e4, pricing_version, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, channel, plan, currency, amountMinor, estimatedE4, feeE4, version, source)
	return err
}

func LatestQuoteSample(channel, plan string) (estimatedE4 int64, at string, ok bool) {
	err := DB.QueryRow(`
		SELECT estimated_usd_e4, COALESCE(created_at,'') FROM x_quote_samples
		WHERE channel = ? AND plan = ? ORDER BY id DESC LIMIT 1
	`, channel, plan).Scan(&estimatedE4, &at)
	return estimatedE4, at, err == nil
}

type XBatchRow struct {
	ID        int64  `json:"id"`
	Plan      string `json:"plan"`
	Channel   string `json:"channel"`
	Quantity  int    `json:"quantity"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	Used      int    `json:"used"`
	Status    string `json:"status"`
}

func ListXBatches(limit int) ([]XBatchRow, error) {
	rows, err := DB.Query(`
		SELECT b.id, b.plan, b.channel, b.quantity, b.note, COALESCE(b.created_at,''),
		       (SELECT COUNT(*) FROM x_codes c WHERE c.batch_id = b.id AND c.status NOT IN ('unused','disabled')),
		       COALESCE(b.status,'issued')
		FROM x_batches b ORDER BY b.id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []XBatchRow
	for rows.Next() {
		var b XBatchRow
		if err := rows.Scan(&b.ID, &b.Plan, &b.Channel, &b.Quantity, &b.Note, &b.CreatedAt, &b.Used, &b.Status); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type XCodeBrief struct {
	ID, BatchID                                                      int64
	Code, Plan, Channel, Status, Note, Recipient, CreatedAt, Message string
	RedemptionID                                                     int64
}

func ListXCodesByBatch(batchID int64) ([]XCode, error) {
	rows, err := DB.Query(`SELECT `+xCodeCols+` FROM x_codes WHERE batch_id = ? ORDER BY id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []XCode
	for rows.Next() {
		var c XCode
		if err := rows.Scan(
			&c.ID, &c.Code, &c.Plan, &c.Channel, &c.AccountID, &c.Status, &c.UpstreamCDKID, &c.UpstreamCodeEnc, &c.UpstreamCodePrefix,
			&c.Currency, &c.MaxOfficialAmountMinor, &c.MaxWalletDebitE4, &c.FundingAmountE4, &c.ServiceFeeE4, &c.PricingVersion,
			&c.DeviceToken, &c.BatchID, &c.IdempotencyKey, &c.Note, &c.CreatedBy, &c.CreatedAt, &c.DisabledAt, &c.CardID,
		); err != nil {
			return nil, err
		}
		c.UpstreamCodeEnc = ""
		out = append(out, c)
	}
	return out, rows.Err()
}

func ListXRecords(group, q string, limit int) ([]map[string]any, error) {
	rows, err := DB.Query(`
		SELECT c.id, c.code, c.plan, c.channel, c.account_id, c.status, c.note, COALESCE(c.created_at,''),
		       COALESCE(r.id,0), COALESCE(r.recipient,''), COALESCE(r.message,''), COALESCE(r.upstream_status,''),
		       COALESCE(r.error_code,''), COALESCE(r.events_json,'[]'), COALESCE(r.resolved_at,''),
		       COALESCE(r.amount_minor,0), COALESCE(r.currency,''), COALESCE(r.estimated_usd_e4,0), COALESCE(r.service_fee_e4,0),
		       COALESCE(r.poll_count,0), COALESCE(r.payment_attempted,0), COALESCE(r.funding_dispatched,0), COALESCE(r.payment_dispatched,0),
		       COALESCE(r.upstream_order_id,''), COALESCE(r.client_request_id,''), COALESCE(r.resolved_note,'')
		FROM x_codes c
		LEFT JOIN x_redemptions r ON r.id = (SELECT id FROM x_redemptions WHERE x_code_id = c.id ORDER BY id DESC LIMIT 1)
		ORDER BY c.id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	q = strings.ToLower(strings.TrimSpace(q))
	var out []map[string]any
	for rows.Next() {
		var id, accountID, rid, amount, est, fee int64
		var poll, pay, fund, dispatched int
		var code, plan, channel, status, note, created, recipient, message, upstream, errCode, events, resolved, currency, orderID, clientReq, resolvedNote string
		if err := rows.Scan(&id, &code, &plan, &channel, &accountID, &status, &note, &created, &rid, &recipient, &message, &upstream, &errCode, &events, &resolved, &amount, &currency, &est, &fee, &poll, &pay, &fund, &dispatched, &orderID, &clientReq, &resolvedNote); err != nil {
			return nil, err
		}
		g := recordGroup(status, errCode, resolved)
		if group != "" && group != "all" && g != group {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(code+" "+recipient+" "+note), q) {
			continue
		}
		out = append(out, map[string]any{
			"code_id": id, "account_id": accountID, "redemption_id": rid, "code": code, "plan": plan, "channel": channel,
			"status": status, "group": g, "note": note, "created_at": created, "recipient": recipient,
			"message": message, "upstream_status": upstream, "error_code": errCode, "events": events,
			"amount_minor": amount, "currency": currency, "estimated_usd_e4": est, "service_fee_e4": fee,
			"poll_count": poll, "payment_attempted": pay != 0, "funding_dispatched": fund != 0, "payment_dispatched": dispatched != 0,
			"upstream_order_id": orderID, "client_request_id": clientReq, "resolved_note": resolvedNote,
		})
	}
	return out, rows.Err()
}

func recordGroup(status, errCode, resolved string) string {
	if resolved != "" && status != "completed" {
		return "done"
	}
	switch status {
	case "unused":
		return "unused"
	case "completed":
		return "done"
	case "disabled":
		return "failed"
	case "uncertain", "review_required", "requires_action":
		return "todo"
	default:
		if errCode == "SPENDABLE_BALANCE_INSUFFICIENT" {
			return "todo"
		}
		return "running"
	}
}

func XOverview() (map[string]any, error) {
	var unused, running, todo, doneToday int
	_ = DB.QueryRow(`SELECT COUNT(*) FROM x_codes WHERE status = 'unused'`).Scan(&unused)
	_ = DB.QueryRow(`SELECT COUNT(*) FROM x_codes WHERE status IN ('quoted','funding','funded','paying','paid_pending_delivery')`).Scan(&running)
	_ = DB.QueryRow(`
		SELECT COUNT(*) FROM x_codes c
		LEFT JOIN x_redemptions r ON r.id = (SELECT id FROM x_redemptions WHERE x_code_id = c.id ORDER BY id DESC LIMIT 1)
		WHERE c.status IN ('uncertain','review_required','requires_action')
		   OR COALESCE(r.error_code,'') = 'SPENDABLE_BALANCE_INSUFFICIENT'
	`).Scan(&todo)
	_ = DB.QueryRow(`SELECT COUNT(*) FROM x_codes WHERE status = 'completed' AND created_at >= date('now')`).Scan(&doneToday)
	var liability int64
	_ = DB.QueryRow(`SELECT COALESCE(SUM(max_wallet_debit_e4),0) FROM x_codes WHERE channel = 'x_cdk' AND status = 'unused'`).Scan(&liability)
	return map[string]any{
		"unused": unused, "running": running, "todo": todo, "done_today": doneToday,
		"cdk_liability_e4": liability,
	}, nil
}

func InsertUpstreamCall(accountID int64, method, path string, status int, detail string) {
	if DB == nil {
		return
	}
	detail = strings.TrimSpace(detail)
	if len(detail) > 300 {
		detail = detail[:300]
	}
	_, _ = DB.Exec(`
		INSERT INTO x_upstream_calls (account_id, method, path, status, detail) VALUES (?, ?, ?, ?, ?)
	`, accountID, method, path, status, detail)
}

func ListUpstreamCalls(accountID int64, limit int) ([]struct {
	Method, Path, Detail, At string
	Status                   int
}, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := DB.Query(`
		SELECT method, path, status, detail, COALESCE(created_at,'')
		FROM x_upstream_calls WHERE account_id = ? ORDER BY id DESC LIMIT ?
	`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Method, Path, Detail, At string
		Status                   int
	}
	for rows.Next() {
		var row struct {
			Method, Path, Detail, At string
			Status                   int
		}
		if err := rows.Scan(&row.Method, &row.Path, &row.Status, &row.Detail, &row.At); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func GetXBatch(id int64) (XBatch, error) {
	var b XBatch
	err := DB.QueryRow(`
		SELECT id, plan, channel, account_id, quantity, note, idempotency_key, created_by,
		       COALESCE(created_at,''), COALESCE(status,'issued'), COALESCE(request_json,'')
		FROM x_batches WHERE id = ?
	`, id).Scan(&b.ID, &b.Plan, &b.Channel, &b.AccountID, &b.Quantity, &b.Note, &b.IdempotencyKey, &b.CreatedBy, &b.CreatedAt, &b.Status, &b.RequestJSON)
	if err == sql.ErrNoRows {
		return b, fmt.Errorf("批次不存在")
	}
	return b, err
}

func SaveXBatchState(id int64, status, requestJSON string) error {
	_, err := DB.Exec(`UPDATE x_batches SET status = ?, request_json = ? WHERE id = ?`, status, requestJSON, id)
	return err
}

func HasXUpstreamCDK(upstreamID string) (bool, error) {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return false, nil
	}
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM x_codes WHERE upstream_cdk_id = ?`, upstreamID).Scan(&n)
	return n > 0, err
}

func CountXQueueAhead(id int64) (int, error) {
	var n int
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM x_redemptions
		WHERE finished_at IS NULL AND id > 0 AND id < ?
		  AND (
		    upstream_status IN ('queued_quote', 'queued_confirm')
		    OR (next_poll_at IS NOT NULL AND next_poll_at <= CURRENT_TIMESTAMP)
		  )
	`, id).Scan(&n)
	return n, err
}

func CountUnusedXCodes(channel string) (int, int64, error) {
	var n int
	var liability int64
	err := DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(max_wallet_debit_e4),0)
		FROM x_codes WHERE channel = ? AND status = 'unused'
	`, channel).Scan(&n, &liability)
	return n, liability, err
}

type XQuoteSample struct {
	Channel        string `json:"channel"`
	Plan           string `json:"plan"`
	Currency       string `json:"currency"`
	AmountMinor    int64  `json:"amount_minor"`
	EstimatedUSDE4 int64  `json:"estimated_usd_e4"`
	ServiceFeeE4   int64  `json:"service_fee_e4"`
	Source         string `json:"source"`
	CreatedAt      string `json:"created_at"`
}

func ListLatestQuoteSamples() ([]XQuoteSample, error) {
	rows, err := DB.Query(`
		SELECT channel, plan, currency, amount_minor, estimated_usd_e4, service_fee_e4, source, COALESCE(created_at,'')
		FROM x_quote_samples
		WHERE id IN (SELECT MAX(id) FROM x_quote_samples GROUP BY channel, plan)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []XQuoteSample
	for rows.Next() {
		var s XQuoteSample
		if err := rows.Scan(&s.Channel, &s.Plan, &s.Currency, &s.AmountMinor, &s.EstimatedUSDE4, &s.ServiceFeeE4, &s.Source, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	AgentOrderStatusPendingPay      = "pending_pay"
	AgentOrderStatusPaid            = "paid"
	AgentOrderStatusIssuing         = "issuing"
	AgentOrderStatusDelivered       = "delivered"
	AgentOrderStatusPaidUndelivered = "paid_undelivered"
	AgentOrderStatusExpired         = "expired"
	AgentOrderStatusCancelled       = "cancelled"

	AgentOrderMaxCount     = 50
	AgentOrderPendingTTL   = 30 * time.Minute
	AgentOrderNoPrefix     = "AG"
)

// AgentOrder 代理购卡订单。
type AgentOrder struct {
	ID                int64    `json:"id"`
	OrderNo           string   `json:"order_no"`
	AgentUserID       int64    `json:"agent_user_id"`
	Plan              string   `json:"plan"`
	PlanLabel         string   `json:"plan_label"`
	Count             int      `json:"count"`
	UnitPriceCents    int64    `json:"unit_price_cents"`
	TotalAmountCents  int64    `json:"total_amount_cents"`
	Status            string   `json:"status"`
	PayType           string   `json:"pay_type"`
	EpayTradeNo       string   `json:"epay_trade_no,omitempty"`
	IssuedCount       int      `json:"issued_count"`
	IssuedCodes       []string `json:"issued_codes,omitempty"`
	FailReason        string   `json:"fail_reason,omitempty"`
	PaidAt            string   `json:"paid_at,omitempty"`
	DeliveredAt       string   `json:"delivered_at,omitempty"`
	ExpiresAt         string   `json:"expires_at,omitempty"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
	AgentUsername     string   `json:"agent_username,omitempty"`
}

type AgentOrderQuery struct {
	AgentUserID int64
	Status      string
	OrderNo     string
	Page        int
	PageSize    int
}

func migrateAgentOrders() error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS agent_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_no TEXT UNIQUE NOT NULL,
			agent_user_id INTEGER NOT NULL,
			plan TEXT NOT NULL,
			plan_label TEXT DEFAULT '',
			count INTEGER NOT NULL,
			unit_price_cents INTEGER NOT NULL,
			total_amount_cents INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending_pay',
			pay_type TEXT DEFAULT '',
			epay_trade_no TEXT DEFAULT '',
			issued_count INTEGER DEFAULT 0,
			issued_codes TEXT DEFAULT '',
			fail_reason TEXT DEFAULT '',
			paid_at DATETIME,
			delivered_at DATETIME,
			expires_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_orders_agent ON agent_orders(agent_user_id, id DESC)`)
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_orders_status ON agent_orders(status, created_at DESC)`)
	return nil
}

func scanAgentOrder(row scanner) (AgentOrder, error) {
	var o AgentOrder
	var issuedCodesRaw, paidAt, deliveredAt, expiresAt sql.NullString
	if err := row.Scan(
		&o.ID, &o.OrderNo, &o.AgentUserID, &o.Plan, &o.PlanLabel, &o.Count,
		&o.UnitPriceCents, &o.TotalAmountCents, &o.Status, &o.PayType, &o.EpayTradeNo,
		&o.IssuedCount, &issuedCodesRaw, &o.FailReason,
		&paidAt, &deliveredAt, &expiresAt, &o.CreatedAt, &o.UpdatedAt,
	); err != nil {
		return o, err
	}
	if paidAt.Valid {
		o.PaidAt = paidAt.String
	}
	if deliveredAt.Valid {
		o.DeliveredAt = deliveredAt.String
	}
	if expiresAt.Valid {
		o.ExpiresAt = expiresAt.String
	}
	if issuedCodesRaw.Valid && strings.TrimSpace(issuedCodesRaw.String) != "" {
		_ = json.Unmarshal([]byte(issuedCodesRaw.String), &o.IssuedCodes)
	}
	return o, nil
}

const agentOrderCols = `id, order_no, agent_user_id, plan, plan_label, count,
	unit_price_cents, total_amount_cents, status, pay_type, epay_trade_no,
	issued_count, issued_codes, fail_reason, paid_at, delivered_at, expires_at, created_at, updated_at`

// CreateAgentOrder 创建待支付订单。
func CreateAgentOrder(o AgentOrder) (AgentOrder, error) {
	if DB == nil {
		return o, fmt.Errorf("db not initialized")
	}
	expires := time.Now().Add(AgentOrderPendingTTL).UTC().Format(time.RFC3339)
	res, err := DB.Exec(`
		INSERT INTO agent_orders (
			order_no, agent_user_id, plan, plan_label, count,
			unit_price_cents, total_amount_cents, status, pay_type, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, o.OrderNo, o.AgentUserID, o.Plan, o.PlanLabel, o.Count,
		o.UnitPriceCents, o.TotalAmountCents, AgentOrderStatusPendingPay, o.PayType, expires)
	if err != nil {
		return o, err
	}
	id, _ := res.LastInsertId()
	return GetAgentOrderByID(id)
}

func GetAgentOrderByID(id int64) (AgentOrder, error) {
	row := DB.QueryRow(`SELECT `+agentOrderCols+` FROM agent_orders WHERE id = ?`, id)
	return scanAgentOrder(row)
}

func GetAgentOrderByNo(orderNo string) (AgentOrder, error) {
	row := DB.QueryRow(`SELECT `+agentOrderCols+` FROM agent_orders WHERE order_no = ?`, strings.TrimSpace(orderNo))
	return scanAgentOrder(row)
}

func GetAgentOrderForAgent(agentID int64, orderNo string) (AgentOrder, error) {
	row := DB.QueryRow(`SELECT `+agentOrderCols+` FROM agent_orders WHERE order_no = ? AND agent_user_id = ?`,
		strings.TrimSpace(orderNo), agentID)
	return scanAgentOrder(row)
}

func parseAgentOrderTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// AgentOrderIsExpired 待支付订单是否已过 expires_at。
func AgentOrderIsExpired(o AgentOrder) bool {
	if o.Status != AgentOrderStatusPendingPay {
		return false
	}
	t, ok := parseAgentOrderTime(o.ExpiresAt)
	if !ok {
		return false
	}
	return time.Now().After(t)
}

// ExpireAgentOrderIfNeeded CAS：pending_pay → expired（超时）。
func ExpireAgentOrderIfNeeded(orderNo string) (AgentOrder, bool, error) {
	if DB == nil {
		return AgentOrder{}, false, fmt.Errorf("db not initialized")
	}
	orderNo = strings.TrimSpace(orderNo)
	o, err := GetAgentOrderByNo(orderNo)
	if err != nil {
		return o, false, err
	}
	if !AgentOrderIsExpired(o) {
		return o, false, nil
	}
	res, err := DB.Exec(`
		UPDATE agent_orders SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE order_no = ? AND status = ?
	`, AgentOrderStatusExpired, orderNo, AgentOrderStatusPendingPay)
	if err != nil {
		return o, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fresh, err := GetAgentOrderByNo(orderNo)
		return fresh, fresh.Status == AgentOrderStatusExpired, err
	}
	o, err = GetAgentOrderByNo(orderNo)
	return o, true, err
}

// ExpireStaleAgentPendingOrders 批量过期超时待支付订单（列表加载时调用）。
func ExpireStaleAgentPendingOrders(agentUserID int64) error {
	if DB == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if agentUserID > 0 {
		_, err := DB.Exec(`
			UPDATE agent_orders SET status = ?, updated_at = CURRENT_TIMESTAMP
			WHERE agent_user_id = ? AND status = ? AND expires_at != '' AND expires_at < ?
		`, AgentOrderStatusExpired, agentUserID, AgentOrderStatusPendingPay, now)
		return err
	}
	_, err := DB.Exec(`
		UPDATE agent_orders SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE status = ? AND expires_at != '' AND expires_at < ?
	`, AgentOrderStatusExpired, AgentOrderStatusPendingPay, now)
	return err
}

// MarkAgentOrderPaid CAS：仅 pending_pay → paid。
func MarkAgentOrderPaid(orderNo, epayTradeNo string) (AgentOrder, bool, error) {
	if DB == nil {
		return AgentOrder{}, false, fmt.Errorf("db not initialized")
	}
	res, err := DB.Exec(`
		UPDATE agent_orders SET
			status = ?, epay_trade_no = ?, paid_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE order_no = ? AND status = ?
	`, AgentOrderStatusPaid, strings.TrimSpace(epayTradeNo), strings.TrimSpace(orderNo), AgentOrderStatusPendingPay)
	if err != nil {
		return AgentOrder{}, false, err
	}
	n, _ := res.RowsAffected()
	o, err := GetAgentOrderByNo(orderNo)
	if err != nil {
		return o, false, err
	}
	return o, n == 1, nil
}

func UpdateAgentOrderIssuing(orderNo string) (bool, error) {
	if DB == nil {
		return false, fmt.Errorf("db not initialized")
	}
	res, err := DB.Exec(`
		UPDATE agent_orders SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE order_no = ? AND status IN (?, ?, ?)
	`, AgentOrderStatusIssuing, orderNo,
		AgentOrderStatusPaid, AgentOrderStatusPaidUndelivered, AgentOrderStatusIssuing)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func CompleteAgentOrderDelivery(orderNo string, codes []string) error {
	if DB == nil {
		return fmt.Errorf("db not initialized")
	}
	raw, _ := json.Marshal(codes)
	_, err := DB.Exec(`
		UPDATE agent_orders SET
			status = ?, issued_count = ?, issued_codes = ?,
			delivered_at = CURRENT_TIMESTAMP, fail_reason = '', updated_at = CURRENT_TIMESTAMP
		WHERE order_no = ?
	`, AgentOrderStatusDelivered, len(codes), string(raw), orderNo)
	return err
}

func FailAgentOrderDelivery(orderNo string, issuedCount int, codes []string, reason string) error {
	if DB == nil {
		return fmt.Errorf("db not initialized")
	}
	raw, _ := json.Marshal(codes)
	status := AgentOrderStatusPaidUndelivered
	if issuedCount >= 0 {
		// partial delivery still undelivered for admin retry
	}
	_, err := DB.Exec(`
		UPDATE agent_orders SET
			status = ?, issued_count = ?, issued_codes = ?,
			fail_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE order_no = ?
	`, status, issuedCount, string(raw), truncate(reason, 500), orderNo)
	return err
}

func ListAgentOrders(q AgentOrderQuery) ([]AgentOrder, int, error) {
	if DB == nil {
		return nil, 0, fmt.Errorf("db not initialized")
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}
	where := []string{"1=1"}
	args := []any{}
	if q.AgentUserID > 0 {
		where = append(where, "agent_user_id = ?")
		args = append(args, q.AgentUserID)
	}
	if st := strings.TrimSpace(q.Status); st != "" {
		where = append(where, "status = ?")
		args = append(args, st)
	}
	if no := strings.TrimSpace(q.OrderNo); no != "" {
		where = append(where, "order_no LIKE ?")
		args = append(args, "%"+no+"%")
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM agent_orders WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (q.Page - 1) * q.PageSize
	listArgs := append(append([]any{}, args...), q.PageSize, offset)
	rows, err := DB.Query(`SELECT `+agentOrderCols+` FROM agent_orders WHERE `+whereSQL+` ORDER BY id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AgentOrder, 0)
	for rows.Next() {
		o, err := scanAgentOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	return out, total, rows.Err()
}

func ListAgentOrdersAdmin(q AgentOrderQuery) ([]AgentOrder, int, error) {
	list, total, err := ListAgentOrders(q)
	if err != nil || len(list) == 0 {
		return list, total, err
	}
	ids := make([]int64, 0, len(list))
	seen := map[int64]bool{}
	for _, o := range list {
		if !seen[o.AgentUserID] {
			ids = append(ids, o.AgentUserID)
			seen[o.AgentUserID] = true
		}
	}
	names := map[int64]string{}
	for _, id := range ids {
		if u, err := GetAgentUserByID(id); err == nil {
			names[id] = u.Username
		}
	}
	for i := range list {
		list[i].AgentUsername = names[list[i].AgentUserID]
	}
	return list, total, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type scanner interface {
	Scan(dest ...any) error
}

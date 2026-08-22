package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// AgentWebhookDelivery 一条待投递/已投递的代理回调。
//
// 采用 outbox：业务侧只管把事件写进这张表，真正的 HTTP 投递由后台 worker 负责。
// 好处是进程重启不丢事件、重试与投递日志天然落库，业务路径也不会被代理端的
// 慢响应拖住。
type AgentWebhookDelivery struct {
	ID             int64  `json:"id"`
	AgentUserID    int64  `json:"-"`
	EventID        string `json:"event_id"`
	EventType      string `json:"event_type"`
	BatchID        string `json:"batch_id,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	Payload        string `json:"-"`
	TargetURL      string `json:"target_url,omitempty"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	NextAttemptAt  string `json:"next_attempt_at,omitempty"`
	LastStatusCode int    `json:"last_status_code"`
	LastError      string `json:"last_error,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	DeliveredAt    string `json:"delivered_at,omitempty"`
}

const agentWebhookCols = `id, agent_user_id, event_id, event_type, COALESCE(batch_id,''), COALESCE(request_id,''),
	payload, COALESCE(target_url,''), COALESCE(status,''), COALESCE(attempts,0),
	COALESCE(next_attempt_at,''), COALESCE(last_status_code,0), COALESCE(last_error,''),
	COALESCE(created_at,''), COALESCE(updated_at,''), COALESCE(delivered_at,'')`

func scanAgentWebhook(s interface{ Scan(...any) error }) (AgentWebhookDelivery, error) {
	var d AgentWebhookDelivery
	err := s.Scan(&d.ID, &d.AgentUserID, &d.EventID, &d.EventType, &d.BatchID, &d.RequestID,
		&d.Payload, &d.TargetURL, &d.Status, &d.Attempts, &d.NextAttemptAt,
		&d.LastStatusCode, &d.LastError, &d.CreatedAt, &d.UpdatedAt, &d.DeliveredAt)
	return d, err
}

// GetAgentWebhookTarget 取代理配置的回调地址与签名密钥。
// 两者缺一不可：没配地址就不投递，没有密钥则无法签名。
func GetAgentWebhookTarget(agentID int64) (url, secret string, err error) {
	if DB == nil || agentID <= 0 {
		return "", "", sql.ErrNoRows
	}
	var u, s sql.NullString
	err = DB.QueryRow(`SELECT webhook_url, webhook_secret FROM agent_users WHERE id = ?`, agentID).Scan(&u, &s)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(u.String), strings.TrimSpace(s.String), nil
}

// EnqueueAgentWebhook 事件入队。event_id 上有唯一索引，重复入队直接忽略，
// 因此调用方可以放心地在可能被重放的路径上调用（例如进程重启后的状态对齐）。
func EnqueueAgentWebhook(agentID int64, eventID, eventType, batchID, requestID, payload, targetURL string) error {
	if DB == nil || agentID <= 0 {
		return nil
	}
	_, err := DB.Exec(`
		INSERT OR IGNORE INTO agent_webhook_deliveries
			(agent_user_id, event_id, event_type, batch_id, request_id, payload, target_url,
			 status, attempts, next_attempt_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, agentID, eventID, eventType, batchID, requestID, payload, targetURL)
	return err
}

// ListDueAgentWebhooks 取到期待投递的事件。worker 单协程消费，不需要行级锁。
func ListDueAgentWebhooks(limit int) ([]AgentWebhookDelivery, error) {
	if DB == nil {
		return nil, nil
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := DB.Query(`SELECT `+agentWebhookCols+`
		FROM agent_webhook_deliveries
		WHERE status = 'pending' AND next_attempt_at <= CURRENT_TIMESTAMP
		ORDER BY next_attempt_at, id
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentWebhookDelivery, 0, limit)
	for rows.Next() {
		d, err := scanAgentWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func MarkAgentWebhookDelivered(id int64, statusCode int) error {
	_, err := DB.Exec(`
		UPDATE agent_webhook_deliveries
		SET status = 'delivered', attempts = attempts + 1, last_status_code = ?, last_error = '',
			delivered_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, statusCode, id)
	return err
}

// MarkAgentWebhookRetry 投递失败但还有重试机会，按 backoffSec 推迟下一次。
func MarkAgentWebhookRetry(id int64, statusCode int, errMsg string, backoffSec int) error {
	if backoffSec < 1 {
		backoffSec = 1
	}
	_, err := DB.Exec(fmt.Sprintf(`
		UPDATE agent_webhook_deliveries
		SET attempts = attempts + 1, last_status_code = ?, last_error = ?,
			next_attempt_at = datetime('now', '+%d seconds'), updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, backoffSec), statusCode, truncateErr(errMsg), id)
	return err
}

// MarkAgentWebhookFailed 重试次数耗尽，落终态。代理可在门户「回调日志」里看到并手动重投。
func MarkAgentWebhookFailed(id int64, statusCode int, errMsg string) error {
	_, err := DB.Exec(`
		UPDATE agent_webhook_deliveries
		SET status = 'failed', attempts = attempts + 1, last_status_code = ?, last_error = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, statusCode, truncateErr(errMsg), id)
	return err
}

// RequeueAgentWebhook 代理在门户里手动重投一条已失败的回调。
func RequeueAgentWebhook(agentID, id int64) error {
	res, err := DB.Exec(`
		UPDATE agent_webhook_deliveries
		SET status = 'pending', attempts = 0, next_attempt_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND agent_user_id = ? AND status = 'failed'
	`, id, agentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListAgentWebhookDeliveries 门户「回调日志」分页。
func ListAgentWebhookDeliveries(agentID int64, page, pageSize int) ([]AgentWebhookDelivery, int, error) {
	if DB == nil || agentID <= 0 {
		return nil, 0, fmt.Errorf("invalid query")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM agent_webhook_deliveries WHERE agent_user_id = ?`,
		agentID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := DB.Query(`SELECT `+agentWebhookCols+`
		FROM agent_webhook_deliveries WHERE agent_user_id = ?
		ORDER BY id DESC LIMIT ? OFFSET ?`, agentID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AgentWebhookDelivery, 0, pageSize)
	for rows.Next() {
		d, err := scanAgentWebhook(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

func truncateErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

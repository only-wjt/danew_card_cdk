package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// AgentBatchSummary 代理可见的批次概览。
// 复用 admin_recharge_batches / admin_recharge_items，靠 agent_user_id 做归属隔离，
// 所有查询都强制带上该条件，代理拿不到别人的批次。
type AgentBatchSummary struct {
	BatchID   string `json:"batch_id"`
	Plan      string `json:"plan"`
	Source    string `json:"source"`
	Total     int    `json:"total"`
	Success   int    `json:"success"`
	Failed    int    `json:"failed"`
	Skipped   int    `json:"skipped"`
	Unknown   int    `json:"unknown"`
	Pending   int    `json:"pending"`
	Running   int    `json:"running"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// agentBatchCols 批次列 + 按状态聚合的明细计数。
const agentBatchCols = `b.batch_id, COALESCE(b.plan,''), COALESCE(b.source,''), b.total,
	COALESCE(b.status,''), COALESCE(b.message,''), COALESCE(b.created_at,''), COALESCE(b.updated_at,''),
	COALESCE((SELECT COUNT(*) FROM admin_recharge_items i WHERE i.batch_id = b.batch_id AND i.status = 'success'), 0),
	COALESCE((SELECT COUNT(*) FROM admin_recharge_items i WHERE i.batch_id = b.batch_id AND i.status = 'failed'), 0),
	COALESCE((SELECT COUNT(*) FROM admin_recharge_items i WHERE i.batch_id = b.batch_id AND i.status = 'skipped'), 0),
	COALESCE((SELECT COUNT(*) FROM admin_recharge_items i WHERE i.batch_id = b.batch_id AND i.status = 'unknown'), 0),
	COALESCE((SELECT COUNT(*) FROM admin_recharge_items i WHERE i.batch_id = b.batch_id AND i.status = 'pending'), 0),
	COALESCE((SELECT COUNT(*) FROM admin_recharge_items i WHERE i.batch_id = b.batch_id
		AND i.status IN ('issuing','preparing','submitted','processing')), 0)`

func scanAgentBatch(s interface{ Scan(...any) error }) (AgentBatchSummary, error) {
	var b AgentBatchSummary
	err := s.Scan(&b.BatchID, &b.Plan, &b.Source, &b.Total, &b.Status, &b.Message, &b.CreatedAt, &b.UpdatedAt,
		&b.Success, &b.Failed, &b.Skipped, &b.Unknown, &b.Pending, &b.Running)
	return b, err
}

// ListAgentRechargeBatches 分页列出代理自己的批次（含单条充值产生的批次）。
func ListAgentRechargeBatches(agentID int64, page, pageSize int) ([]AgentBatchSummary, int, error) {
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
	if err := DB.QueryRow(`SELECT COUNT(*) FROM admin_recharge_batches WHERE agent_user_id = ?`,
		agentID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := DB.Query(`SELECT `+agentBatchCols+`
		FROM admin_recharge_batches b
		WHERE b.agent_user_id = ?
		ORDER BY b.created_at DESC, b.batch_id DESC
		LIMIT ? OFFSET ?`, agentID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AgentBatchSummary, 0, pageSize)
	for rows.Next() {
		b, err := scanAgentBatch(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

// GetAgentRechargeBatch 取单个批次；不属于该代理时返回 nil, nil。
func GetAgentRechargeBatch(agentID int64, batchID string) (*AgentBatchSummary, error) {
	if DB == nil || agentID <= 0 {
		return nil, fmt.Errorf("invalid query")
	}
	b, err := scanAgentBatch(DB.QueryRow(`SELECT `+agentBatchCols+`
		FROM admin_recharge_batches b
		WHERE b.agent_user_id = ? AND b.batch_id = ?`, agentID, strings.TrimSpace(batchID)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// LookupAgentItemForEvent 按 client_request_id 反查明细及其所属代理。
// 批次不属于任何代理（管理端自己发起的）时返回 agentID = 0，调用方据此跳过回调。
func LookupAgentItemForEvent(requestID string) (int64, *AgentRechargeRecord, error) {
	if DB == nil {
		return 0, nil, fmt.Errorf("db not initialized")
	}
	var agentID int64
	var r AgentRechargeRecord
	var cdkCode string
	err := DB.QueryRow(`
		SELECT COALESCE(b.agent_user_id,0), i.batch_id, i.seq, i.client_request_id,
			COALESCE(i.client_reference,''), COALESCE(i.plan,''), COALESCE(i.cred_mode,''),
			COALESCE(i.account_email,''), COALESCE(i.cdk_code,''), COALESCE(i.upstream_order_id,''),
			COALESCE(i.status,''), COALESCE(i.message,''), COALESCE(b.source,''),
			COALESCE(i.created_at,''), COALESCE(i.updated_at,'')
		FROM admin_recharge_items i
		JOIN admin_recharge_batches b ON b.batch_id = i.batch_id
		WHERE i.client_request_id = ?
	`, strings.TrimSpace(requestID)).Scan(&agentID, &r.BatchID, &r.Seq, &r.RequestID, &r.ClientReference,
		&r.Plan, &r.CredMode, &r.AccountEmail, &cdkCode, &r.UpstreamOrderID, &r.Status, &r.Message,
		&r.Source, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	r.CDKPrefix = cdkDisplayPrefix(cdkCode)
	return agentID, &r, nil
}

// LookupBatchAgent 取批次归属代理，0 表示管理端批次。
func LookupBatchAgent(batchID string) (int64, error) {
	if DB == nil {
		return 0, nil
	}
	var agentID int64
	err := DB.QueryRow(`SELECT COALESCE(agent_user_id,0) FROM admin_recharge_batches WHERE batch_id = ?`,
		strings.TrimSpace(batchID)).Scan(&agentID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return agentID, err
}

// ListAgentRechargeBatchItems 批次内明细，按 seq 升序。复用 AgentRechargeRecord，
// 凭据字段一律不在其中，代理只拿得到对账所需信息。
func ListAgentRechargeBatchItems(agentID int64, batchID string) ([]AgentRechargeRecord, error) {
	if DB == nil || agentID <= 0 {
		return nil, fmt.Errorf("invalid query")
	}
	rows, err := DB.Query(`
		SELECT i.batch_id, i.seq, i.client_request_id, COALESCE(i.client_reference,''),
			COALESCE(i.plan,''), COALESCE(i.cred_mode,''), COALESCE(i.account_email,''),
			COALESCE(i.cdk_code,''), COALESCE(i.upstream_order_id,''), COALESCE(i.status,''),
			COALESCE(i.message,''), COALESCE(b.source,''), COALESCE(i.created_at,''), COALESCE(i.updated_at,'')
		FROM admin_recharge_items i
		JOIN admin_recharge_batches b ON b.batch_id = i.batch_id
		WHERE b.agent_user_id = ? AND i.batch_id = ?
		ORDER BY i.seq
	`, agentID, strings.TrimSpace(batchID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentRechargeRecord, 0)
	for rows.Next() {
		var r AgentRechargeRecord
		var cdkCode string
		if err := rows.Scan(&r.BatchID, &r.Seq, &r.RequestID, &r.ClientReference, &r.Plan, &r.CredMode,
			&r.AccountEmail, &cdkCode, &r.UpstreamOrderID, &r.Status, &r.Message, &r.Source,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.CDKPrefix = cdkDisplayPrefix(cdkCode)
		out = append(out, r)
	}
	return out, rows.Err()
}

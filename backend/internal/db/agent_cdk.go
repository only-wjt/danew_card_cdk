package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrCDKNotFound      = errors.New("卡密不存在或未录入本站")
	ErrCDKNotAssigned   = errors.New("卡密未分配给当前代理")
	ErrCDKWrongAgent    = errors.New("卡密已分配给其他代理")
	ErrCDKUnavailable   = errors.New("卡密不可用（已使用或已禁用）")
	ErrCDKPlanMismatch  = errors.New("卡密套餐与所选套餐不一致")
	ErrCDKInFlight      = errors.New("卡密正在其他充值任务中使用")
	ErrCDKDuplicate     = errors.New("本批卡密重复")
	ErrCDKLocalStock    = errors.New("GPT白号不能用于代充，请复制发给下级")
)

// StoredCDK 本站缓存的卡密详情。
type StoredCDK struct {
	UpstreamID          int64
	Code                string
	Plan                string
	Prefix              string
	Status              string
	AssignedAgentUserID int64
}

func migrateAgentCDKInventory() error {
	if DB == nil {
		return nil
	}
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cardplatform_cdk_codes') WHERE name='assigned_agent_user_id'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := DB.Exec(`ALTER TABLE cardplatform_cdk_codes ADD COLUMN assigned_agent_user_id INTEGER DEFAULT 0`); err != nil {
			return err
		}
	}
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cp_cdk_assigned_agent ON cardplatform_cdk_codes(assigned_agent_user_id)`)
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cp_cdk_plan_agent_status ON cardplatform_cdk_codes(plan, assigned_agent_user_id, status)`)
	_, _ = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_ari_cdk_code ON admin_recharge_items(cdk_code)`)
	return nil
}

func cdkStatusUsable(status string) bool {
	st := strings.ToLower(strings.TrimSpace(status))
	return st == "" || st == "unused"
}

// AgentCDKInventoryItem 代理名下的一张卡密（含完整码，仅供本人查看与复制）。
type AgentCDKInventoryItem struct {
	Code      string `json:"code"`
	Prefix    string `json:"code_prefix,omitempty"`
	Plan      string `json:"plan"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// AgentCDKInventoryQuery 代理卡密库存查询。
type AgentCDKInventoryQuery struct {
	AgentUserID int64
	Status      string // unused / reserved / consumed / 空=全部
	Plan        string
	Code        string // 前缀或片段模糊
	Page        int
	PageSize    int
}

// AgentCDKInventorySummary 代理卡密库存汇总。
type AgentCDKInventorySummary struct {
	Total    int `json:"total"`
	Unused   int `json:"unused"`
	Reserved int `json:"reserved"`
	Consumed int `json:"consumed"`
}

func agentCDKStatusWhere(status string) (clause string, args []interface{}) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "unused", "available":
		return `(status = '' OR lower(status) = 'unused')`, nil
	case "reserved", "in_flight":
		return `lower(status) = 'reserved'`, nil
	case "consumed", "used":
		return `lower(status) = 'consumed'`, nil
	default:
		return "", nil
	}
}

// AgentCDKInventorySummaryFor 统计代理名下各状态卡密数量。
func AgentCDKInventorySummaryFor(agentUserID int64) (AgentCDKInventorySummary, error) {
	out := AgentCDKInventorySummary{}
	if DB == nil || agentUserID <= 0 {
		return out, fmt.Errorf("invalid query")
	}
	rows, err := DB.Query(`
		SELECT lower(COALESCE(status,'')), COUNT(*)
		FROM cardplatform_cdk_codes
		WHERE assigned_agent_user_id = ?
		GROUP BY lower(COALESCE(status,''))
	`, agentUserID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return out, err
		}
		out.Total += n
		switch status {
		case "", "unused":
			out.Unused += n
		case "reserved":
			out.Reserved += n
		case "consumed":
			out.Consumed += n
		}
	}
	return out, rows.Err()
}

// ListAgentCDKInventory 分页列出代理名下卡密（含完整码）。
func ListAgentCDKInventory(q AgentCDKInventoryQuery) ([]AgentCDKInventoryItem, int, error) {
	if DB == nil || q.AgentUserID <= 0 {
		return nil, 0, fmt.Errorf("invalid query")
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 50
	}
	offset := (q.Page - 1) * q.PageSize

	where := []string{"assigned_agent_user_id = ?"}
	args := []interface{}{q.AgentUserID}
	if stClause, stArgs := agentCDKStatusWhere(q.Status); stClause != "" {
		where = append(where, stClause)
		args = append(args, stArgs...)
	}
	if p := strings.TrimSpace(q.Plan); p != "" {
		where = append(where, "plan = ?")
		args = append(args, p)
	}
	if c := strings.TrimSpace(q.Code); c != "" {
		where = append(where, "(code LIKE ? OR code_prefix LIKE ?)")
		args = append(args, c+"%", "%"+c+"%")
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM cardplatform_cdk_codes WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append(append([]interface{}{}, args...), q.PageSize, offset)
	rows, err := DB.Query(`
		SELECT code, COALESCE(code_prefix,''), COALESCE(plan,''),
		       COALESCE(status,''), COALESCE(created_at,'')
		FROM cardplatform_cdk_codes
		WHERE `+whereSQL+`
		ORDER BY
			CASE lower(COALESCE(status,''))
				WHEN '' THEN 0 WHEN 'unused' THEN 0
				WHEN 'reserved' THEN 1
				ELSE 2
			END,
			created_at DESC, code ASC
		LIMIT ? OFFSET ?
	`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]AgentCDKInventoryItem, 0)
	for rows.Next() {
		var it AgentCDKInventoryItem
		if err := rows.Scan(&it.Code, &it.Prefix, &it.Plan, &it.Status, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		if strings.TrimSpace(it.Status) == "" {
			it.Status = "unused"
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// LookupStoredCDKDetail 按完整码查本站缓存（含代理归属）。
func LookupStoredCDKDetail(code string) (StoredCDK, bool) {
	code = normalizeCDKCode(code)
	if code == "" || DB == nil {
		return StoredCDK{}, false
	}
	var row StoredCDK
	err := DB.QueryRow(`
		SELECT COALESCE(upstream_id,0), code, COALESCE(plan,''), COALESCE(code_prefix,''),
		       COALESCE(status,''), COALESCE(assigned_agent_user_id,0)
		FROM cardplatform_cdk_codes
		WHERE upper(trim(code)) = upper(trim(?))
		ORDER BY created_at DESC LIMIT 1
	`, code).Scan(&row.UpstreamID, &row.Code, &row.Plan, &row.Prefix, &row.Status, &row.AssignedAgentUserID)
	if err != nil {
		return StoredCDK{}, false
	}
	return row, true
}

// IsCDKInFlight 卡密是否挂在未终态的充值明细上。
func IsCDKInFlight(code string) bool {
	if DB == nil {
		return false
	}
	code = normalizeCDKCode(code)
	if code == "" {
		return false
	}
	statuses := AgentInFlightItemStatuses
	ph := make([]string, len(statuses))
	args := make([]any, 0, len(statuses)+1)
	args = append(args, code)
	for i, st := range statuses {
		ph[i] = "?"
		args = append(args, st)
	}
	var n int
	q := `SELECT COUNT(*) FROM admin_recharge_items
		WHERE upper(trim(cdk_code)) = upper(trim(?))
		  AND status IN (` + strings.Join(ph, ",") + `)`
	_ = DB.QueryRow(q, args...).Scan(&n)
	return n > 0
}

// AccountEmailByCDK 反查卡密最终充值到的账号邮箱。
// 代理/管理端批量代充不落 session，客户凭卡密查账单时靠这个邮箱走 accounthub。
func AccountEmailByCDK(code string) string {
	if DB == nil {
		return ""
	}
	code = normalizeCDKCode(code)
	if code == "" {
		return ""
	}
	var email string
	err := DB.QueryRow(`
		SELECT COALESCE(account_email,'') FROM admin_recharge_items
		WHERE upper(trim(cdk_code)) = upper(trim(?)) AND COALESCE(account_email,'') <> ''
		ORDER BY CASE WHEN status = 'success' THEN 0 ELSE 1 END, created_at DESC
		LIMIT 1
	`, code).Scan(&email)
	if err == nil && strings.TrimSpace(email) != "" {
		return strings.TrimSpace(email)
	}
	err = DB.QueryRow(`
		SELECT COALESCE(account_email,'') FROM recharge_tasks
		WHERE upper(trim(cdk_code)) = upper(trim(?)) AND COALESCE(account_email,'') <> ''
		ORDER BY created_at DESC LIMIT 1
	`, code).Scan(&email)
	if err == nil {
		return strings.TrimSpace(email)
	}
	return ""
}

// CheckAgentCDKForRecharge 校验代理提交的卡密是否可用（不预留）。
func CheckAgentCDKForRecharge(agentUserID int64, plan, code string) error {
	code = normalizeCDKCode(code)
	if code == "" {
		return fmt.Errorf("缺少卡密")
	}
	row, ok := LookupStoredCDKDetail(code)
	if !ok {
		return ErrCDKNotFound
	}
	if IsLocalStockPlan(row.Plan) {
		return ErrCDKLocalStock
	}
	if row.AssignedAgentUserID == 0 {
		return ErrCDKNotAssigned
	}
	if row.AssignedAgentUserID != agentUserID {
		return ErrCDKWrongAgent
	}
	if !cdkStatusUsable(row.Status) {
		return ErrCDKUnavailable
	}
	if p := strings.TrimSpace(plan); p != "" && row.Plan != "" && !strings.EqualFold(row.Plan, p) {
		return ErrCDKPlanMismatch
	}
	if IsCDKInFlight(code) {
		return ErrCDKInFlight
	}
	hash := HashCDKCode(code)
	if AgentCDKAlreadyExchanged(hash) {
		return ErrCDKUnavailable
	}
	return nil
}

// AgentCDKValidateIssue 单条卡密校验结果（用于批量预检）。
type AgentCDKValidateIssue struct {
	Line      int    `json:"line"`
	Code      string `json:"code"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

// AgentCDKValidateSummary 批量卡密预检汇总。
type AgentCDKValidateSummary struct {
	TotalLines     int                     `json:"total_lines"`
	EmptySkipped   int                     `json:"empty_skipped"`
	DuplicateLines int                     `json:"duplicate_lines"`
	UniqueCount    int                     `json:"unique_count"`
	ValidCount     int                     `json:"valid_count"`
	InvalidCount   int                     `json:"invalid_count"`
	ValidCodes     []string                `json:"valid_codes"`
	Duplicates     []AgentCDKValidateIssue `json:"duplicates"`
	Invalid        []AgentCDKValidateIssue `json:"invalid"`
}

// AgentCDKErrorCode 把校验错误映射为 API error_code。
func AgentCDKErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrCDKNotFound):
		return "CDK_NOT_FOUND"
	case errors.Is(err, ErrCDKNotAssigned):
		return "CDK_NOT_ASSIGNED"
	case errors.Is(err, ErrCDKWrongAgent):
		return "CDK_WRONG_AGENT"
	case errors.Is(err, ErrCDKPlanMismatch):
		return "CDK_PLAN_MISMATCH"
	case errors.Is(err, ErrCDKInFlight):
		return "CDK_IN_FLIGHT"
	case errors.Is(err, ErrCDKUnavailable):
		return "CDK_UNAVAILABLE"
	case errors.Is(err, ErrCDKDuplicate):
		return "CDK_DUPLICATE"
	case errors.Is(err, ErrCDKLocalStock):
		return "CDK_LOCAL_STOCK"
	default:
		return "CDK_INVALID"
	}
}

// AgentCDKErrorMessage 人类可读说明。
func AgentCDKErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrCDKNotFound):
		return "卡密不存在或未录入本站"
	case errors.Is(err, ErrCDKNotAssigned):
		return "卡密尚未分配给你，请联系站长"
	case errors.Is(err, ErrCDKWrongAgent):
		return "卡密已分配给其他代理"
	case errors.Is(err, ErrCDKPlanMismatch):
		return "卡密套餐与所选套餐不一致"
	case errors.Is(err, ErrCDKInFlight):
		return "卡密正在其他充值任务中使用"
	case errors.Is(err, ErrCDKUnavailable):
		return "卡密不可用（已使用或已禁用）"
	case errors.Is(err, ErrCDKDuplicate):
		return "卡密重复"
	case errors.Is(err, ErrCDKLocalStock):
		return "GPT白号不能用于代充，请复制发给下级"
	default:
		return err.Error()
	}
}

func cdkValidateDisplay(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if len(code) <= 18 {
		return code
	}
	return code[:14] + "…"
}

// ValidateAgentCDKBatch 批量预检：去重 + 逐条校验，不预留卡密。
func ValidateAgentCDKBatch(agentUserID int64, plan string, lines []string) AgentCDKValidateSummary {
	out := AgentCDKValidateSummary{
		ValidCodes: make([]string, 0),
		Duplicates: make([]AgentCDKValidateIssue, 0),
		Invalid:    make([]AgentCDKValidateIssue, 0),
	}
	seen := map[string]int{} // normalized code -> first line (1-based)
	for i, raw := range lines {
		lineNo := i + 1
		code := strings.TrimSpace(raw)
		if code == "" {
			out.EmptySkipped++
			continue
		}
		out.TotalLines++
		norm := normalizeCDKCode(code)
		if first, dup := seen[norm]; dup {
			out.DuplicateLines++
			out.Duplicates = append(out.Duplicates, AgentCDKValidateIssue{
				Line:      lineNo,
				Code:      cdkValidateDisplay(code),
				ErrorCode: "CDK_DUPLICATE",
				Message:   fmt.Sprintf("与第 %d 行重复", first),
			})
			continue
		}
		seen[norm] = lineNo
		out.UniqueCount++

		if err := CheckAgentCDKForRecharge(agentUserID, plan, code); err != nil {
			out.InvalidCount++
			out.Invalid = append(out.Invalid, AgentCDKValidateIssue{
				Line:      lineNo,
				Code:      cdkValidateDisplay(code),
				ErrorCode: AgentCDKErrorCode(err),
				Message:   AgentCDKErrorMessage(err),
			})
			continue
		}
		row, ok := LookupStoredCDKDetail(code)
		if !ok {
			out.InvalidCount++
			out.Invalid = append(out.Invalid, AgentCDKValidateIssue{
				Line: lineNo, Code: cdkValidateDisplay(code),
				ErrorCode: "CDK_NOT_FOUND", Message: "卡密不存在或未录入本站",
			})
			continue
		}
		out.ValidCount++
		out.ValidCodes = append(out.ValidCodes, row.Code)
	}
	return out
}

// ReserveAgentCDK 将卡密标为 reserved（创建批次时调用）。
func ReserveAgentCDK(code string) error {
	row, ok := LookupStoredCDKDetail(code)
	if !ok {
		return ErrCDKNotFound
	}
	return UpdateCardplatformCDKStatus(row.UpstreamID, "reserved")
}

// ReleaseAgentCDK 失败/跳过后释放卡密，允许代理重试。
func ReleaseAgentCDK(code string) error {
	row, ok := LookupStoredCDKDetail(code)
	if !ok {
		return nil
	}
	if strings.EqualFold(row.Status, "consumed") {
		return nil
	}
	return UpdateCardplatformCDKStatus(row.UpstreamID, "unused")
}

// ConsumeAgentCDK 充值成功后标记卡密已消耗。
func ConsumeAgentCDK(code string) error {
	row, ok := LookupStoredCDKDetail(code)
	if !ok {
		return nil
	}
	return UpdateCardplatformCDKStatus(row.UpstreamID, "consumed")
}

// AssignCDKsToAgent 把未分配且可用的卡密批量划给代理（站长线下发货后录入）。
func AssignCDKsToAgent(agentUserID int64, codes []string) (assigned int, skipped []string, err error) {
	if DB == nil {
		return 0, nil, fmt.Errorf("db not ready")
	}
	if agentUserID <= 0 {
		return 0, nil, fmt.Errorf("invalid agent")
	}
	tx, err := DB.Begin()
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	for _, raw := range codes {
		code := normalizeCDKCode(raw)
		if code == "" {
			continue
		}
		var upstreamID, assignedTo int64
		var status string
		qerr := tx.QueryRow(`
			SELECT upstream_id, COALESCE(assigned_agent_user_id,0), COALESCE(status,'')
			FROM cardplatform_cdk_codes
			WHERE upper(trim(code)) = upper(trim(?))
			ORDER BY created_at DESC LIMIT 1
		`, code).Scan(&upstreamID, &assignedTo, &status)
		if qerr == sql.ErrNoRows {
			skipped = append(skipped, code+": 未录入本站")
			continue
		}
		if qerr != nil {
			return assigned, skipped, qerr
		}
		if assignedTo > 0 && assignedTo != agentUserID {
			skipped = append(skipped, code+": 已分配给其他代理")
			continue
		}
		if assignedTo == agentUserID {
			skipped = append(skipped, code+": 已在该代理名下")
			continue
		}
		if !cdkStatusUsable(status) {
			skipped = append(skipped, code+": 状态不可用")
			continue
		}
		if IsCDKInFlight(code) {
			skipped = append(skipped, code+": 正在充值中")
			continue
		}
		if _, err := tx.Exec(`UPDATE cardplatform_cdk_codes SET assigned_agent_user_id = ? WHERE upstream_id = ?`,
			agentUserID, upstreamID); err != nil {
			return assigned, skipped, err
		}
		assigned++
	}
	if err := tx.Commit(); err != nil {
		return 0, skipped, err
	}
	return assigned, skipped, nil
}

// UnassignCDKsFromAgent 收回代理名下的卡密。codes 为空表示收回该代理全部未使用的卡密。
// 已消耗或正在充值中的码不动，避免影响在途订单和对账。
func UnassignCDKsFromAgent(agentUserID int64, codes []string) (released int, skipped []string, err error) {
	if DB == nil {
		return 0, nil, fmt.Errorf("db not ready")
	}
	if agentUserID <= 0 {
		return 0, nil, fmt.Errorf("invalid agent")
	}

	targets := make([]string, 0, len(codes))
	for _, raw := range codes {
		if code := normalizeCDKCode(raw); code != "" {
			targets = append(targets, code)
		}
	}
	if len(targets) == 0 {
		rows, qerr := DB.Query(`
			SELECT code FROM cardplatform_cdk_codes
			WHERE assigned_agent_user_id = ? AND (status = '' OR lower(status) = 'unused')
		`, agentUserID)
		if qerr != nil {
			return 0, nil, qerr
		}
		defer rows.Close()
		for rows.Next() {
			var code string
			if serr := rows.Scan(&code); serr != nil {
				return 0, nil, serr
			}
			targets = append(targets, normalizeCDKCode(code))
		}
		if rerr := rows.Err(); rerr != nil {
			return 0, nil, rerr
		}
	}

	for _, code := range targets {
		row, ok := LookupStoredCDKDetail(code)
		if !ok {
			skipped = append(skipped, code+": 未录入本站")
			continue
		}
		if row.AssignedAgentUserID != agentUserID {
			skipped = append(skipped, code+": 不在该代理名下")
			continue
		}
		if !cdkStatusUsable(row.Status) {
			skipped = append(skipped, code+": 已使用或不可用")
			continue
		}
		if IsCDKInFlight(code) {
			skipped = append(skipped, code+": 正在充值中")
			continue
		}
		if _, uerr := DB.Exec(`UPDATE cardplatform_cdk_codes SET assigned_agent_user_id = 0 WHERE upstream_id = ?`,
			row.UpstreamID); uerr != nil {
			return released, skipped, uerr
		}
		released++
	}
	return released, skipped, nil
}

// CountAgentUnusedCDKs 代理名下仍可用（未消耗）的卡密数量。
func CountAgentUnusedCDKs(agentUserID int64) (int, error) {
	if DB == nil || agentUserID <= 0 {
		return 0, nil
	}
	var n int
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM cardplatform_cdk_codes
		WHERE assigned_agent_user_id = ?
		  AND (status = '' OR lower(status) = 'unused')
	`, agentUserID).Scan(&n)
	return n, err
}

// AgentCDKStock 代理卡密库存概览。
type AgentCDKStock struct {
	AgentUserID int64 `json:"agent_user_id"`
	Total       int   `json:"total"`
	Unused      int   `json:"unused"`
	Reserved    int   `json:"reserved"`
	Consumed    int   `json:"consumed"`
}

// AgentCDKStockMap 一次算出所有代理的卡密库存，避免列表页 N+1 查询。
func AgentCDKStockMap() (map[int64]AgentCDKStock, error) {
	out := map[int64]AgentCDKStock{}
	if DB == nil {
		return out, nil
	}
	rows, err := DB.Query(`
		SELECT assigned_agent_user_id, lower(COALESCE(status,'')), COUNT(*)
		FROM cardplatform_cdk_codes
		WHERE COALESCE(assigned_agent_user_id,0) > 0
		GROUP BY assigned_agent_user_id, lower(COALESCE(status,''))
	`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var agentID int64
		var status string
		var n int
		if err := rows.Scan(&agentID, &status, &n); err != nil {
			return out, err
		}
		s := out[agentID]
		s.AgentUserID = agentID
		s.Total += n
		switch status {
		case "", "unused":
			s.Unused += n
		case "reserved":
			s.Reserved += n
		case "consumed":
			s.Consumed += n
		}
		out[agentID] = s
	}
	return out, rows.Err()
}

// ReconcileCDKAfterItemTerminal 明细进入终态后同步卡密库存状态。
func ReconcileCDKAfterItemTerminal(cdkCode, itemStatus string) {
	code := normalizeCDKCode(cdkCode)
	if code == "" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(itemStatus)) {
	case "success", "skipped":
		_ = ConsumeAgentCDK(code)
	case "unknown":
		// 结果不确定：禁止自动释放，避免双花
	case "failed":
		_ = ReleaseAgentCDK(code)
	default:
	}
}

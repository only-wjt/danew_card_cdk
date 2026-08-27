package db

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	PlanGPTWhite              = "gpt_white"
	PlanGPTWhiteLabel         = "GPT白号"
	PlanGPTWhiteDefaultCents  = int64(300) // ¥3.00
	FulfillmentLocalStock     = "local_stock"
	FulfillmentCardPlatform   = "card_platform"
	localStockImportMaxItems  = 500
	localStockCodeMaxRunes    = 512
)

// IsLocalStockPlan 本站库存 SKU（不走卡台发码）。
func IsLocalStockPlan(plan string) bool {
	return strings.EqualFold(strings.TrimSpace(plan), PlanGPTWhite)
}

// CanonicalPlanKey 归一化档位 key。本站库存档位大小写不敏感，落库与查询都用小写，
// 否则「按 GPT_WHITE 下单、按 gpt_white 入库」会在代理付款之后才查不到库存。
func CanonicalPlanKey(plan string) string {
	plan = strings.TrimSpace(plan)
	if IsLocalStockPlan(plan) {
		return PlanGPTWhite
	}
	return plan
}

// LocalStockPlanLabel 本地库存档位展示名。
func LocalStockPlanLabel(plan string) string {
	if IsLocalStockPlan(plan) {
		return PlanGPTWhiteLabel
	}
	return strings.TrimSpace(plan)
}

// PlanFulfillment 购卡履约方式。
func PlanFulfillment(plan string) string {
	if IsLocalStockPlan(plan) {
		return FulfillmentLocalStock
	}
	return FulfillmentCardPlatform
}

func normalizeLocalStockCode(raw string) string {
	return strings.TrimSpace(raw)
}

func localStockCodePrefix(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if utf8.RuneCountInString(code) <= 14 {
		return code
	}
	var b strings.Builder
	n := 0
	for _, r := range code {
		b.WriteRune(r)
		n++
		if n >= 14 {
			break
		}
	}
	return b.String()
}

// ParseLocalStockImportLines 一行一个账号：email 或 email:password。
func ParseLocalStockImportLines(text string) []string {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if code := normalizeLocalStockCode(line); code != "" {
			out = append(out, code)
		}
	}
	return out
}

func nextLocalStockUpstreamID(tx *sql.Tx) (int64, error) {
	var minID *int64
	if err := tx.QueryRow(`SELECT MIN(upstream_id) FROM cardplatform_cdk_codes`).Scan(&minID); err != nil {
		return 0, err
	}
	next := int64(-1)
	if minID != nil && *minID < 0 {
		next = *minID - 1
	}
	return next, nil
}

// ImportLocalStockCodes 导入本站库存（未分配、unused）。已存在的码跳过，不覆盖。
func ImportLocalStockCodes(plan string, codes []string) (imported int, skipped []string, err error) {
	if DB == nil {
		return 0, nil, fmt.Errorf("db not ready")
	}
	if !IsLocalStockPlan(plan) {
		return 0, nil, fmt.Errorf("仅支持导入 GPT白号")
	}
	plan = CanonicalPlanKey(plan)
	if len(codes) > localStockImportMaxItems {
		return 0, nil, fmt.Errorf("单次最多导入 %d 条", localStockImportMaxItems)
	}

	seen := map[string]bool{}
	clean := make([]string, 0, len(codes))
	for _, raw := range codes {
		code := normalizeLocalStockCode(raw)
		if code == "" {
			continue
		}
		if utf8.RuneCountInString(code) > localStockCodeMaxRunes {
			skipped = append(skipped, truncateForSkip(code)+": 过长")
			continue
		}
		key := strings.ToUpper(code)
		if seen[key] {
			skipped = append(skipped, truncateForSkip(code)+": 本批重复")
			continue
		}
		seen[key] = true
		clean = append(clean, code)
	}
	if len(clean) == 0 {
		return 0, skipped, nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	for _, code := range clean {
		var existing string
		qerr := tx.QueryRow(`
			SELECT code FROM cardplatform_cdk_codes
			WHERE upper(trim(code)) = upper(trim(?))
			LIMIT 1
		`, code).Scan(&existing)
		if qerr == nil {
			skipped = append(skipped, truncateForSkip(code)+": 已存在")
			continue
		}

		upstreamID, idErr := nextLocalStockUpstreamID(tx)
		if idErr != nil {
			return imported, skipped, idErr
		}
		prefix := localStockCodePrefix(code)
		if _, err := tx.Exec(`
			INSERT INTO cardplatform_cdk_codes (upstream_id, code, code_prefix, plan, fee_amount_minor, status, created_at)
			VALUES (?, ?, ?, ?, 0, 'unused', CURRENT_TIMESTAMP)
		`, upstreamID, code, prefix, plan); err != nil {
			return imported, skipped, err
		}
		imported++
	}
	if err := tx.Commit(); err != nil {
		return imported, skipped, err
	}
	return imported, skipped, nil
}

func truncateForSkip(code string) string {
	code = strings.TrimSpace(code)
	if utf8.RuneCountInString(code) <= 24 {
		return code
	}
	var b strings.Builder
	n := 0
	for _, r := range code {
		b.WriteRune(r)
		n++
		if n >= 20 {
			break
		}
	}
	b.WriteString("…")
	return b.String()
}

// ClaimUnassignedLocalStock 从本站未分配库存划给代理，原子占用。
func ClaimUnassignedLocalStock(agentUserID int64, plan string, qty int) ([]string, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	if agentUserID <= 0 {
		return nil, fmt.Errorf("invalid agent")
	}
	if !IsLocalStockPlan(plan) {
		return nil, fmt.Errorf("not a local stock plan")
	}
	plan = CanonicalPlanKey(plan)
	if qty < 1 {
		return nil, fmt.Errorf("count must be >= 1")
	}

	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`
		SELECT code FROM cardplatform_cdk_codes
		WHERE plan = ?
		  AND (status = '' OR lower(status) = 'unused')
		  AND COALESCE(assigned_agent_user_id, 0) = 0
		ORDER BY created_at ASC, rowid ASC
		LIMIT ?
	`, plan, qty)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, qty)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return nil, err
		}
		code = strings.TrimSpace(code)
		if code != "" {
			codes = append(codes, code)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if len(codes) < qty {
		return nil, fmt.Errorf("GPT白号库存不足：需要 %d 条，仅剩 %d 条", qty, len(codes))
	}

	for _, code := range codes {
		res, err := tx.Exec(`
			UPDATE cardplatform_cdk_codes
			SET assigned_agent_user_id = ?
			WHERE upper(trim(code)) = upper(trim(?))
			  AND COALESCE(assigned_agent_user_id, 0) = 0
			  AND (status = '' OR lower(status) = 'unused')
		`, agentUserID, code)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return nil, fmt.Errorf("库存争抢失败，请重试")
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// CountUnassignedLocalStock 某本地档位仍可售（未分配、未使用）的数量。
// 下单前预检用：库存不够就别收钱，否则只能落到 paid_undelivered 再人工退。
func CountUnassignedLocalStock(plan string) (int, error) {
	if DB == nil {
		return 0, fmt.Errorf("db not ready")
	}
	if !IsLocalStockPlan(plan) {
		return 0, fmt.Errorf("not a local stock plan")
	}
	var n int
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM cardplatform_cdk_codes
		WHERE plan = ?
		  AND (status = '' OR lower(status) = 'unused')
		  AND COALESCE(assigned_agent_user_id, 0) = 0
	`, CanonicalPlanKey(plan)).Scan(&n)
	return n, err
}

// LocalStockSummary 某本地档位的库存概况。
type LocalStockSummary struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Total      int    `json:"total"`
	Unassigned int    `json:"unassigned"`
	Assigned   int    `json:"assigned"`
}

// ListLocalStockSummaries 统计本地库存档位。
func ListLocalStockSummaries() ([]LocalStockSummary, error) {
	out := []LocalStockSummary{{
		Key:   PlanGPTWhite,
		Label: PlanGPTWhiteLabel,
	}}
	if DB == nil {
		return out, nil
	}
	rows, err := DB.Query(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN COALESCE(assigned_agent_user_id, 0) = 0
				AND (status = '' OR lower(status) = 'unused') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN COALESCE(assigned_agent_user_id, 0) > 0 THEN 1 ELSE 0 END), 0)
		FROM cardplatform_cdk_codes
		WHERE plan = ?
	`, PlanGPTWhite)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	if rows.Next() {
		var total, unassigned, assigned int
		if err := rows.Scan(&total, &unassigned, &assigned); err != nil {
			return out, err
		}
		out[0].Total = total
		out[0].Unassigned = unassigned
		out[0].Assigned = assigned
	}
	return out, rows.Err()
}

func seedLocalStockDefaultPrices() error {
	defaults, err := GetAgentDefaultPlanPrices()
	if err != nil {
		return err
	}
	if defaults == nil {
		defaults = AgentPlanPriceMap{}
	}
	if _, ok := defaults[PlanGPTWhite]; ok {
		return nil
	}
	defaults[PlanGPTWhite] = PlanGPTWhiteDefaultCents
	return SetAgentDefaultPlanPrices(defaults)
}

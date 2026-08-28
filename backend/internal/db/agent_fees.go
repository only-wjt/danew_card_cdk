package db

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	// agentDefaultPlanPricesKey 全局默认套餐代理价（分），JSON：plan_key → 分。
	agentDefaultPlanPricesKey = "agent_default_plan_prices"
	agentPlanPriceMaxCents    = int64(10_000_000) // ¥100,000
)

// AgentPlanPriceMap 套餐代理价（人民币分）。键存在即视为已设置（含显式 0）。
type AgentPlanPriceMap map[string]int64

func migrateAgentPlanFees() error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS agent_plan_fees (
			agent_user_id INTEGER NOT NULL,
			plan_key TEXT NOT NULL,
			price_cny_cents INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (agent_user_id, plan_key)
		)
	`)
	if err != nil {
		return err
	}
	// 旧列 fee_usd → price_cny_cents（未提交分支上的历史库）
	var hasUSD, hasCNY int
	_ = DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('agent_plan_fees') WHERE name='fee_usd'`).Scan(&hasUSD)
	_ = DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('agent_plan_fees') WHERE name='price_cny_cents'`).Scan(&hasCNY)
	if hasUSD > 0 && hasCNY == 0 {
		if _, err := DB.Exec(`ALTER TABLE agent_plan_fees ADD COLUMN price_cny_cents INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if hasUSD > 0 {
		// 历史 fee_usd 列按分拷贝到 price_cny_cents（未迁移行）
		_, _ = DB.Exec(`
			UPDATE agent_plan_fees
			SET price_cny_cents = fee_usd
			WHERE COALESCE(price_cny_cents, 0) = 0 AND COALESCE(fee_usd, 0) > 0
		`)
	}
	if _, err = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_plan_fees_agent ON agent_plan_fees(agent_user_id)`); err != nil {
		return err
	}
	return seedLocalStockDefaultPrices()
}

// GetAgentDefaultPlanPrices 读取全局默认套餐代理价（分）。
func GetAgentDefaultPlanPrices() (AgentPlanPriceMap, error) {
	out := AgentPlanPriceMap{}
	if DB == nil {
		return out, nil
	}
	raw, err := GetSetting(agentDefaultPlanPricesKey)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(raw) != "" {
		return decodePlanPriceMapCents(raw)
	}
	// 兼容旧键（元浮点），读一次后不再写回旧键
	raw, _ = GetSetting("agent_default_open_fees")
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	return decodePlanPriceMapYuan(raw)
}

// SetAgentDefaultPlanPrices 整表替换全局默认套餐代理价（分）。
func SetAgentDefaultPlanPrices(prices AgentPlanPriceMap) error {
	if DB == nil {
		return fmt.Errorf("db not initialized")
	}
	normalized, err := normalizePlanPriceMap(prices)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		_ = DeleteSetting("agent_default_open_fees")
		return DeleteSetting(agentDefaultPlanPricesKey)
	}
	b, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	return SetSetting(agentDefaultPlanPricesKey, string(b))
}

// GetAgentPlanPriceOverrides 读取某代理的套餐价覆盖（分）。
func GetAgentPlanPriceOverrides(agentID int64) (AgentPlanPriceMap, error) {
	out := AgentPlanPriceMap{}
	if DB == nil || agentID <= 0 {
		return out, nil
	}
	rows, err := DB.Query(`
		SELECT plan_key, COALESCE(price_cny_cents, 0) FROM agent_plan_fees WHERE agent_user_id = ?
	`, agentID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var cents int64
		if err := rows.Scan(&key, &cents); err != nil {
			return out, err
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = cents
	}
	return out, rows.Err()
}

// ReplaceAgentPlanPriceOverrides 整表替换某代理的套餐价覆盖（分）。
func ReplaceAgentPlanPriceOverrides(agentID int64, prices AgentPlanPriceMap) error {
	if DB == nil {
		return fmt.Errorf("db not initialized")
	}
	if agentID <= 0 {
		return fmt.Errorf("invalid agent id")
	}
	normalized, err := normalizePlanPriceMap(prices)
	if err != nil {
		return err
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM agent_plan_fees WHERE agent_user_id = ?`, agentID); err != nil {
		return err
	}
	for key, cents := range normalized {
		if _, err := tx.Exec(`
			INSERT INTO agent_plan_fees (agent_user_id, plan_key, price_cny_cents, updated_at)
			VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		`, agentID, key, cents); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func planPriceLookup(m AgentPlanPriceMap, plan string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return 0, false
	}
	if cents, ok := m[plan]; ok {
		return cents, true
	}
	// 历史库可能仍存 pro，统一查 pro_20x 时回落。
	if plan == "pro_20x" {
		if cents, ok := m["pro"]; ok {
			return cents, true
		}
	}
	return 0, false
}

// EffectiveAgentPlanPrice 套餐代理价（分）：覆盖 → 全局默认 → 0。
func EffectiveAgentPlanPrice(plan string, overrides, defaults AgentPlanPriceMap) int64 {
	plan = CanonicalPlanKey(plan)
	if plan == "" {
		return 0
	}
	if cents, ok := planPriceLookup(overrides, plan); ok {
		return cents
	}
	if cents, ok := planPriceLookup(defaults, plan); ok {
		return cents
	}
	return 0
}

// EffectiveAgentPlanPrices 按档位列表计算有效价（分）。
func EffectiveAgentPlanPrices(plans []string, overrides, defaults AgentPlanPriceMap) AgentPlanPriceMap {
	out := AgentPlanPriceMap{}
	for _, p := range plans {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out[p] = EffectiveAgentPlanPrice(p, overrides, defaults)
	}
	return out
}

// LoadAgentEffectivePlanPrices 读库后计算某代理在给定档位上的有效代理价（分）。
func LoadAgentEffectivePlanPrices(agentID int64, plans []string) (effective, overrides, defaults AgentPlanPriceMap, err error) {
	defaults, err = GetAgentDefaultPlanPrices()
	if err != nil {
		return nil, nil, nil, err
	}
	overrides, err = GetAgentPlanPriceOverrides(agentID)
	if err != nil {
		return nil, nil, defaults, err
	}
	return EffectiveAgentPlanPrices(plans, overrides, defaults), overrides, defaults, nil
}

func decodePlanPriceMapCents(raw string) (AgentPlanPriceMap, error) {
	return decodePlanPriceMap(raw, jsonToCentsDirect)
}

func decodePlanPriceMapYuan(raw string) (AgentPlanPriceMap, error) {
	return decodePlanPriceMap(raw, jsonYuanToCents)
}

func decodePlanPriceMap(raw string, parse func(any) (int64, bool)) (AgentPlanPriceMap, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AgentPlanPriceMap{}, nil
	}
	var tmp map[string]any
	if err := json.Unmarshal([]byte(raw), &tmp); err != nil {
		return nil, fmt.Errorf("invalid plan price json")
	}
	out := AgentPlanPriceMap{}
	for k, v := range tmp {
		cents, ok := parse(v)
		if !ok {
			return nil, fmt.Errorf("invalid price for plan %s", k)
		}
		out[k] = cents
	}
	return normalizePlanPriceMap(out)
}

func normalizePlanPriceMap(in AgentPlanPriceMap) (AgentPlanPriceMap, error) {
	out := AgentPlanPriceMap{}
	if in == nil {
		return out, nil
	}
	for rawKey, cents := range in {
		raw := strings.TrimSpace(rawKey)
		if raw == "" {
			return nil, fmt.Errorf("plan key required")
		}
		if len(raw) > 64 || !isSafePlanKey(raw) {
			return nil, fmt.Errorf("invalid plan key: %s", raw)
		}
		if cents < 0 || cents > agentPlanPriceMaxCents {
			return nil, fmt.Errorf("price for %s must be between 0 and %d cents", raw, agentPlanPriceMaxCents)
		}
		key := CanonicalPlanKey(raw)
		if prev, ok := out[key]; ok {
			// 显式 pro_20x 覆盖别名 pro；两边都是别名时保留先写入的。
			if strings.EqualFold(raw, key) {
				out[key] = cents
			} else {
				_ = prev
			}
			continue
		}
		out[key] = cents
	}
	return out, nil
}

// NormalizePlanPricesFromYuan 管理端表单（元）→ 分。
func NormalizePlanPricesFromYuan(yuan map[string]float64) (AgentPlanPriceMap, error) {
	out := AgentPlanPriceMap{}
	for k, v := range yuan {
		out[k] = YuanToPriceCents(v)
	}
	return normalizePlanPriceMap(out)
}

// YuanToPriceCents 将元字符串/浮点转为分（四舍五入到分）。
func YuanToPriceCents(yuan float64) int64 {
	if math.IsNaN(yuan) || math.IsInf(yuan, 0) {
		return 0
	}
	return int64(math.Round(yuan * 100))
}

func jsonYuanToCents(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return YuanToPriceCents(n), true
	case json.Number:
		f, err := n.Float64()
		return YuanToPriceCents(f), err == nil
	case int:
		return YuanToPriceCents(float64(n)), true
	case int64:
		return YuanToPriceCents(float64(n)), true
	default:
		return 0, false
	}
}

func jsonToCentsDirect(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(math.Round(n)), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, true
		}
		f, err := n.Float64()
		return int64(math.Round(f)), err == nil
	case int:
		return int64(n), true
	case int64:
		return n, true
	default:
		return 0, false
	}
}

func isSafePlanKey(key string) bool {
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

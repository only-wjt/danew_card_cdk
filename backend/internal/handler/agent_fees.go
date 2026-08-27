package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/epay"
	"github.com/gin-gonic/gin"
)

type agentPlanFeeBody struct {
	Fees map[string]float64 `json:"fees"` // 元
}

func planPriceSources(plans []pricedPlanMeta, overrides, defaults db.AgentPlanPriceMap) []gin.H {
	out := make([]gin.H, 0, len(plans)+len(overrides)+len(defaults))
	seen := map[string]bool{}
	appendPlan := func(key, label string, isCredit bool) {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		if strings.TrimSpace(label) == "" {
			label = key
		}
		source := "fallback"
		if _, ok := overrides[key]; ok {
			source = "override"
		} else if _, ok := defaults[key]; ok {
			source = "default"
		}
		cents := db.EffectiveAgentPlanPrice(key, overrides, defaults)
		out = append(out, gin.H{
			"key":              key,
			"label":            label,
			"price_cny_cents":  cents,
			"price_yuan":       epay.MoneyYuan(cents),
			"source":           source,
			"is_credit":        isCredit,
		})
	}
	for _, p := range plans {
		appendPlan(p.Key, p.Label, p.IsCredit)
	}
	for k := range overrides {
		appendPlan(k, "", strings.HasPrefix(k, "credit"))
	}
	for k := range defaults {
		appendPlan(k, "", strings.HasPrefix(k, "credit"))
	}
	return out
}

func priceMapJSON(m db.AgentPlanPriceMap) gin.H {
	out := gin.H{}
	for k, v := range m {
		out[k] = epay.MoneyYuan(v)
	}
	return out
}

func priceMapCentsJSON(m db.AgentPlanPriceMap) gin.H {
	out := gin.H{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// AdminGetAgentDefaultPlanFees GET /api/v1/admin/agent-plan-fees
func AdminGetAgentDefaultPlanFees(c *gin.Context) {
	defaults, err := db.GetAgentDefaultPlanPrices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	plans, live := pricingCatalog(c)
	c.JSON(http.StatusOK, gin.H{
		"fees":              priceMapJSON(defaults),
		"fees_cents":        priceMapCentsJSON(defaults),
		"plans":             planPriceSources(plans, nil, defaults),
		"catalog_source":    catalogSource(live),
		"currency":          "CNY",
	})
}

// AdminPutAgentDefaultPlanFees PUT /api/v1/admin/agent-plan-fees
func AdminPutAgentDefaultPlanFees(c *gin.Context) {
	var req agentPlanFeeBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Fees == nil {
		req.Fees = map[string]float64{}
	}
	prices, err := db.NormalizePlanPricesFromYuan(req.Fees)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.SetAgentDefaultPlanPrices(prices); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "agent_default_plan_prices", "plans="+strconv.Itoa(len(req.Fees)))
	AdminGetAgentDefaultPlanFees(c)
}

// AdminGetAgentPlanFees GET /api/v1/admin/agents/:id/plan-fees
func AdminGetAgentPlanFees(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := db.GetAgentUserByID(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "代理不存在"})
		return
	}
	plans, live := pricingCatalog(c)
	keys := make([]string, 0, len(plans))
	for _, p := range plans {
		keys = append(keys, p.Key)
	}
	effective, overrides, defaults, err := db.LoadAgentEffectivePlanPrices(id, keys)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"defaults":       priceMapJSON(defaults),
		"overrides":      priceMapJSON(overrides),
		"effective":      priceMapJSON(effective),
		"defaults_cents": priceMapCentsJSON(defaults),
		"overrides_cents": priceMapCentsJSON(overrides),
		"effective_cents": priceMapCentsJSON(effective),
		"plans":          planPriceSources(plans, overrides, defaults),
		"catalog_source": catalogSource(live),
		"currency":       "CNY",
	})
}

// AdminPutAgentPlanFees PUT /api/v1/admin/agents/:id/plan-fees
func AdminPutAgentPlanFees(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := db.GetAgentUserByID(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "代理不存在"})
		return
	}
	var req agentPlanFeeBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Fees == nil {
		req.Fees = map[string]float64{}
	}
	prices, err := db.NormalizePlanPricesFromYuan(req.Fees)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.ReplaceAgentPlanPriceOverrides(id, prices); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "agent_plan_prices", "id="+strconv.FormatInt(id, 10)+" plans="+strconv.Itoa(len(req.Fees)))
	AdminGetAgentPlanFees(c)
}

// pricedPlanMeta 管理端「套餐代理价」表单用的档位行。
type pricedPlanMeta struct {
	Key      string
	Label    string
	IsCredit bool
}

func coreSellableFallbackPlans() []pricedPlanMeta {
	return []pricedPlanMeta{
		{Key: "plus", Label: "Plus"},
		{Key: "pro_5x", Label: "Pro 5x"},
		{Key: "pro_20x", Label: "Pro 20x"},
		{Key: "credit250", Label: "Codex 点数 250", IsCredit: true},
		{Key: "credit500", Label: "Codex 点数 500", IsCredit: true},
		{Key: "credit1000", Label: "Codex 点数 1000", IsCredit: true},
	}
}

func localStockPlans() []pricedPlanMeta {
	return []pricedPlanMeta{{Key: db.PlanGPTWhite, Label: db.PlanGPTWhiteLabel}}
}

func corePricedPlans() []pricedPlanMeta {
	core := coreSellableFallbackPlans()
	out := make([]pricedPlanMeta, 0, len(core)+3)
	out = append(out, core[:3]...)
	out = append(out, pricedPlanMeta{Key: "pro", Label: "Pro"}, pricedPlanMeta{Key: "go", Label: "Go"})
	out = append(out, localStockPlans()...)
	out = append(out, core[3:]...)
	return out
}

func liveSellablePlans(c *gin.Context) ([]cardplatform.SellablePlan, bool) {
	cli := cardplatform.NewFromSettings()
	plans, err := cli.GetPlans(c.Request.Context())
	if err != nil || plans == nil {
		return nil, false
	}
	live := plans.SellablePlans()
	if len(live) == 0 {
		return nil, false
	}
	return live, true
}

func mergePricedPlans(core []pricedPlanMeta, live []cardplatform.SellablePlan) []pricedPlanMeta {
	seen := map[string]int{}
	out := make([]pricedPlanMeta, 0, len(core)+len(live))
	for _, p := range core {
		key := strings.TrimSpace(p.Key)
		if key == "" {
			continue
		}
		p.Key = key
		if strings.TrimSpace(p.Label) == "" {
			p.Label = key
		}
		seen[key] = len(out)
		out = append(out, p)
	}
	for _, p := range live {
		key := strings.TrimSpace(p.Key)
		if key == "" {
			continue
		}
		label := strings.TrimSpace(p.Label)
		if i, ok := seen[key]; ok {
			if label != "" {
				out[i].Label = label
			}
			out[i].IsCredit = p.IsCredit
			continue
		}
		if label == "" {
			label = key
		}
		seen[key] = len(out)
		out = append(out, pricedPlanMeta{Key: key, Label: label, IsCredit: p.IsCredit})
	}
	return out
}

func pricingCatalog(c *gin.Context) (plans []pricedPlanMeta, live bool) {
	livePlans, ok := liveSellablePlans(c)
	return mergePricedPlans(corePricedPlans(), livePlans), ok
}

func resolveAgentPlanCatalog(c *gin.Context, agent *db.AgentUser) []pricedPlanMeta {
	live, ok := liveSellablePlans(c)
	var catalog []pricedPlanMeta
	if ok {
		catalog = mergePricedPlans(localStockPlans(), live)
	} else {
		catalog = append(localStockPlans(), coreSellableFallbackPlans()...)
	}
	out := make([]pricedPlanMeta, 0, len(catalog))
	for _, p := range catalog {
		if agentPlanAllowed(agent, p.Key, nil) {
			out = append(out, p)
		}
	}
	return out
}

func catalogSource(live bool) string {
	if live {
		return "live"
	}
	return "core"
}

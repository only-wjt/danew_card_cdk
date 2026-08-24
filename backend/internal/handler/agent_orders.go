package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/epay"
	"github.com/gin-gonic/gin"
)

func loadEpayConfig() epay.Config {
	base := strings.TrimSpace(settingOr("epay_api_base", ""))
	signMode := strings.TrimSpace(settingOr("epay_sign_mode", ""))
	if signMode == "" && strings.Contains(strings.ToLower(base), "payqixiang") {
		signMode = "append"
	}
	return epay.Config{
		APIBase:  base,
		PID:      strings.TrimSpace(settingOr("epay_pid", "")),
		Key:      strings.TrimSpace(settingOr("epay_key", "")),
		SignMode: signMode,
	}
}

func sitePublicBase(c *gin.Context) string {
	return ResolvePublicBase(c)
}

func newAgentOrderNo() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s%d%s", db.AgentOrderNoPrefix, time.Now().Unix(), hex.EncodeToString(b))
}

func agentOrderJSON(o db.AgentOrder) gin.H {
	return gin.H{
		"id":                 o.ID,
		"order_no":           o.OrderNo,
		"plan":               o.Plan,
		"plan_label":         o.PlanLabel,
		"count":              o.Count,
		"unit_price_cents":   o.UnitPriceCents,
		"total_amount_cents": o.TotalAmountCents,
		"unit_price_yuan":    epay.MoneyYuan(o.UnitPriceCents),
		"total_amount_yuan":  epay.MoneyYuan(o.TotalAmountCents),
		"status":             o.Status,
		"pay_type":           o.PayType,
		"issued_count":       o.IssuedCount,
		"issued_codes":       o.IssuedCodes,
		"fail_reason":        o.FailReason,
		"paid_at":            o.PaidAt,
		"delivered_at":       o.DeliveredAt,
		"expires_at":         o.ExpiresAt,
		"created_at":         o.CreatedAt,
		"updated_at":         o.UpdatedAt,
		"agent_username":     o.AgentUsername,
	}
}

func buildAgentOrderPayURL(c *gin.Context, order db.AgentOrder) (string, error) {
	cfg := loadEpayConfig()
	if !cfg.Ready() {
		return "", fmt.Errorf("易支付未配置")
	}
	base := sitePublicBase(c)
	notifyURL := base + "/api/v1/webhooks/epay"
	returnURL := base + "/partner/orders?paid=1&order_no=" + order.OrderNo
	label := strings.TrimSpace(order.PlanLabel)
	if label == "" {
		label = order.Plan
	}
	name := fmt.Sprintf("CDK %s x%d", label, order.Count)
	result, err := cfg.CreateMapiPay(
		c.Request.Context(),
		order.OrderNo,
		name,
		epay.MoneyYuan(order.TotalAmountCents),
		notifyURL,
		returnURL,
		order.PayType,
		c.ClientIP(),
	)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result.PayURL) != "" {
		return result.PayURL, nil
	}
	return result.QRCode, nil
}

// AgentCreateOrder POST /api/v1/agent/orders
func AgentCreateOrder(c *gin.Context) {
	var req struct {
		Plan    string `json:"plan"`
		Count   int    `json:"count"`
		PayType string `json:"pay_type"` // alipay | wxpay，须已在 epay_pay_types 中开通
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	plan := strings.TrimSpace(req.Plan)
	if plan == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择套餐"})
		return
	}
	count := req.Count
	if count < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数量至少为 1"})
		return
	}
	if count > db.AgentOrderMaxCount {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("单次最多购买 %d 张", db.AgentOrderMaxCount)})
		return
	}
	allowedPayTypes := epay.ParsePayTypes(settingOr("epay_pay_types", "alipay"))
	payType := strings.ToLower(strings.TrimSpace(req.PayType))
	if payType == "" {
		payType = allowedPayTypes[0]
	}
	if !epay.AllowedPayType(payType, allowedPayTypes) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该支付方式未开通"})
		return
	}

	agentID := agentUserID(c)
	agent, err := db.GetAgentUserByID(agentID)
	if err != nil || agent.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "账号不可用"})
		return
	}
	if !agentPlanAllowed(agent, plan, agentSellableKeys(c.Request.Context())) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无权购买该套餐"})
		return
	}

	epayCfg := loadEpayConfig()
	if !epayCfg.Ready() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "易支付未配置，请联系站长"})
		return
	}
	if cardplatform.LoadConfig().APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "卡台未配置，暂无法自动发码"})
		return
	}

	catalog := resolveAgentPlanCatalog(c, agent)
	label := plan
	for _, p := range catalog {
		if p.Key == plan {
			label = p.Label
			break
		}
	}
	keys := []string{plan}
	effective, _, _, err := db.LoadAgentEffectivePlanPrices(agentID, keys)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	unitCents := effective[plan]
	if unitCents <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该套餐尚未定价，请联系站长"})
		return
	}
	totalCents := unitCents * int64(count)

	orderNo := newAgentOrderNo()
	order, err := db.CreateAgentOrder(db.AgentOrder{
		OrderNo:          orderNo,
		AgentUserID:      agentID,
		Plan:             plan,
		PlanLabel:        label,
		Count:            count,
		UnitPriceCents:   unitCents,
		TotalAmountCents: totalCents,
		PayType:          payType,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	payURL, err := buildAgentOrderPayURL(c, order)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order":   agentOrderJSON(order),
		"pay_url": payURL,
	})
}

// AgentRepayOrder POST /api/v1/agent/orders/:order_no/repay — 继续支付原订单
func AgentRepayOrder(c *gin.Context) {
	orderNo := strings.TrimSpace(c.Param("order_no"))
	agentID := agentUserID(c)
	o, err := db.GetAgentOrderForAgent(agentID, orderNo)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "订单不存在"})
		return
	}
	o, _, _ = db.ExpireAgentOrderIfNeeded(orderNo)
	if o.Status == db.AgentOrderStatusExpired {
		c.JSON(http.StatusBadRequest, gin.H{"error": "订单已过期，请重新下单"})
		return
	}
	if o.Status != db.AgentOrderStatusPendingPay {
		c.JSON(http.StatusBadRequest, gin.H{"error": "订单不可继续支付"})
		return
	}
	payURL, err := buildAgentOrderPayURL(c, o)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"order": agentOrderJSON(o), "pay_url": payURL})
}

// AgentListOrders GET /api/v1/agent/orders
func AgentListOrders(c *gin.Context) {
	agentID := agentUserID(c)
	_ = db.ExpireStaleAgentPendingOrders(agentID)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := db.ListAgentOrders(db.AgentOrderQuery{
		AgentUserID: agentID,
		Status:      strings.TrimSpace(c.Query("status")),
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, o := range list {
		out = append(out, agentOrderJSON(o))
	}
	c.JSON(http.StatusOK, gin.H{"list": out, "total": total, "page": page, "page_size": pageSize})
}

// AgentGetOrder GET /api/v1/agent/orders/:order_no
func AgentGetOrder(c *gin.Context) {
	orderNo := strings.TrimSpace(c.Param("order_no"))
	o, err := db.GetAgentOrderForAgent(agentUserID(c), orderNo)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "订单不存在"})
		return
	}
	o, _, _ = db.ExpireAgentOrderIfNeeded(orderNo)
	c.JSON(http.StatusOK, agentOrderJSON(o))
}

// EpayNotify GET|POST /api/v1/webhooks/epay
func EpayNotify(c *gin.Context) {
	params := map[string]string{}
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	if err := c.Request.ParseForm(); err == nil {
		for k, vals := range c.Request.PostForm {
			if len(vals) > 0 {
				params[k] = vals[0]
			}
		}
	}

	cfg := loadEpayConfig()
	if !cfg.Ready() {
		c.String(http.StatusServiceUnavailable, "fail")
		return
	}
	ok, msg := cfg.VerifyNotify(params)
	if !ok {
		log.Printf("[epay] notify verify fail: %s params=%v", msg, params)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	orderNo := strings.TrimSpace(params["out_trade_no"])
	if !strings.HasPrefix(orderNo, db.AgentOrderNoPrefix) {
		c.String(http.StatusOK, "success")
		return
	}

	moneyCents, err := epay.ParseMoneyYuan(params["money"])
	if err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}

	order, err := db.GetAgentOrderByNo(orderNo)
	if err != nil {
		log.Printf("[epay] unknown order=%s", orderNo)
		c.String(http.StatusOK, "success")
		return
	}

	order, _, _ = db.ExpireAgentOrderIfNeeded(orderNo)
	if order.Status == db.AgentOrderStatusExpired {
		log.Printf("[epay] expired order paid notify order=%s", orderNo)
		c.String(http.StatusOK, "success")
		return
	}

	if moneyCents != order.TotalAmountCents {
		log.Printf("[epay] amount mismatch order=%s want=%d got=%d", orderNo, order.TotalAmountCents, moneyCents)
		c.String(http.StatusOK, "success")
		return
	}

	order, marked, err := db.MarkAgentOrderPaid(orderNo, params["trade_no"])
	if err != nil {
		log.Printf("[epay] mark paid err order=%s: %v", orderNo, err)
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	if !marked {
		// 已处理过：若已发货直接 success
		if order.Status == db.AgentOrderStatusDelivered {
			c.String(http.StatusOK, "success")
			return
		}
		if order.Status == db.AgentOrderStatusPaidUndelivered {
			order, err = fulfillAgentOrder(c.Request.Context(), order)
			if err != nil {
				log.Printf("[epay] retry fulfill fail order=%s: %v", orderNo, err)
			}
			c.String(http.StatusOK, "success")
			return
		}
		c.String(http.StatusOK, "success")
		return
	}

	db.WriteAudit("epay", "agent_order_paid", orderNo+" cents="+strconv.FormatInt(moneyCents, 10), c.ClientIP())

	if _, err := fulfillAgentOrder(c.Request.Context(), order); err != nil {
		log.Printf("[epay] fulfill fail order=%s: %v", orderNo, err)
	}
	c.String(http.StatusOK, "success")
}

// AdminListAgentOrders GET /api/v1/admin/agent-orders
func AdminListAgentOrders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	agentID, _ := strconv.ParseInt(c.Query("agent_id"), 10, 64)
	list, total, err := db.ListAgentOrdersAdmin(db.AgentOrderQuery{
		AgentUserID: agentID,
		Status:      strings.TrimSpace(c.Query("status")),
		OrderNo:     strings.TrimSpace(c.Query("order_no")),
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, o := range list {
		out = append(out, agentOrderJSON(o))
	}
	c.JSON(http.StatusOK, gin.H{"list": out, "total": total, "page": page, "page_size": pageSize})
}

// AdminRetryAgentOrder POST /api/v1/admin/agent-orders/:order_no/retry
func AdminRetryAgentOrder(c *gin.Context) {
	orderNo := strings.TrimSpace(c.Param("order_no"))
	order, err := db.GetAgentOrderByNo(orderNo)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "订单不存在"})
		return
	}
	if order.Status != db.AgentOrderStatusPaidUndelivered &&
		order.Status != db.AgentOrderStatusPaid &&
		order.Status != db.AgentOrderStatusIssuing {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前状态不可重试发货"})
		return
	}
	order, err = fulfillAgentOrder(c.Request.Context(), order)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "order": agentOrderJSON(order)})
		return
	}
	auditAdmin(c, "agent_order_retry", orderNo)
	c.JSON(http.StatusOK, gin.H{"ok": true, "order": agentOrderJSON(order)})
}

// AdminEpayTest POST /api/v1/admin/epay/test — 用 mapi.php 校验 PID/Key 与签名
func AdminEpayTest(c *gin.Context) {
	cfg := loadEpayConfig()
	if !cfg.Ready() {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "易支付未配置完整（网关、PID、Key）"})
		return
	}
	base := sitePublicBase(c)
	orderNo := fmt.Sprintf("TEST%d", time.Now().Unix())
	result, err := cfg.CreateMapiPay(
		c.Request.Context(),
		orderNo,
		"配置测试",
		"0.01",
		base+"/api/v1/webhooks/epay",
		base+"/partner/orders",
		"alipay",
		c.ClientIP(),
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"message":  "签名校验通过，易支付配置正确",
		"trade_no": result.TradeNo,
		"payurl":   result.PayURL,
	})
}

package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danew/cdk-recharge-system/internal/agentwebhook"
	"github.com/danew/cdk-recharge-system/internal/auth"
	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/epay"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func agentUserID(c *gin.Context) int64 {
	v, _ := c.Get("agent_user_id")
	id, _ := v.(int64)
	return id
}

// agentSellableKeys 卡台当前可发码的档位集合。拉不到时返回 nil，
// 此时不把「档位不可售」当成拒绝理由，交由后续卡台调用自己报错。
func agentSellableKeys(ctx context.Context) map[string]bool {
	cli := cardplatform.NewFromSettings()
	plans, err := cli.GetPlans(ctx)
	if err != nil || plans == nil {
		return nil
	}
	return plans.SellableKeys()
}

// agentPlanAllowed 档位可见性 = 卡台可售 ∩ 代理白名单。
// 代理白名单为空表示「跟卡台走」，不在代理侧另维护一份档位清单
// （与 internal/cardplatform/sellable.go 的判据保持一致）。
func agentPlanAllowed(agent *db.AgentUser, plan string, sellable map[string]bool) bool {
	plan = db.CanonicalPlanKey(plan)
	if plan == "" {
		return false
	}
	if db.IsLocalStockPlan(plan) {
		return true
	}
	if len(sellable) > 0 && !planSellable(plan, sellable) {
		return false
	}
	if agent == nil || len(agent.AllowedPlans) == 0 {
		return true
	}
	for _, p := range agent.AllowedPlans {
		if db.CanonicalPlanKey(p) == plan {
			return true
		}
	}
	return false
}

// planSellable 卡台可售判定；pro 与 pro_20x 互通。
func planSellable(plan string, sellable map[string]bool) bool {
	if len(sellable) == 0 {
		return true
	}
	plan = db.CanonicalPlanKey(plan)
	if sellable[plan] {
		return true
	}
	if plan == "pro_20x" && sellable["pro"] {
		return true
	}
	return false
}

// resolveCardIssuePlan 发码时把统一后的 pro_20x 映射回卡台实际键（若上游只开了 pro）。
func resolveCardIssuePlan(plan string, sellable map[string]bool) string {
	plan = db.CanonicalPlanKey(plan)
	if plan == "pro_20x" && len(sellable) > 0 && !sellable["pro_20x"] && sellable["pro"] {
		return "pro"
	}
	return plan
}

// ---- 代理 API 限流 ----

type agentRateWindow struct {
	start time.Time
	count int
}

var (
	agentRateMu   sync.Mutex
	agentRateHits = make(map[int64]*agentRateWindow)
)

// agentRateAllow 每个代理每分钟的调用配额（agent_users.rate_limit_rpm）。
func agentRateAllow(agentID int64, rpm int) bool {
	if rpm <= 0 {
		rpm = 60
	}
	agentRateMu.Lock()
	defer agentRateMu.Unlock()

	now := time.Now()
	w := agentRateHits[agentID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		agentRateHits[agentID] = &agentRateWindow{start: now, count: 1}
		return true
	}
	if w.count >= rpm {
		return false
	}
	w.count++
	return true
}

// AgentRateLimit 挂在整个 /agent 路由组上，按代理维度限流。
func AgentRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := agentUserID(c)
		if id <= 0 {
			c.Next()
			return
		}
		rpm := 60
		if agent, err := db.GetAgentUserByID(id); err == nil {
			rpm = agent.RateLimitRPM
		}
		if !agentRateAllow(id, rpm) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":      "请求过于频繁，请稍后重试",
				"error_code": "RATE_LIMITED",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ---- Auth ----

type agentLoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func AgentLogin(c *gin.Context) {
	var req agentLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	rateKey := c.ClientIP() + "|agent|" + strings.ToLower(strings.TrimSpace(req.Username))
	if ok, wait := loginAllowed(rateKey); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("尝试次数过多，请 %d 分钟后再试", int(wait.Minutes())+1),
		})
		return
	}
	agent, hash, err := db.GetAgentUserByUsername(req.Username)
	if err != nil {
		loginFailed(rateKey)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if agent.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "账号已被停用"})
		return
	}
	plain := strings.TrimSpace(req.Password)
	ok, upgraded := db.VerifyAdminPassword(hash, plain)
	if !ok {
		loginFailed(rateKey)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	loginSucceeded(rateKey)
	db.UpgradeAgentHash(agent.Username, upgraded)

	expiration := time.Now().Add(24 * time.Hour)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.CustomClaims{
		UserID:   agent.ID,
		IsAdmin:  false,
		IsAgent:  true,
		Username: agent.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiration),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   agent.Username,
		},
	})
	signed, err := token.SignedString([]byte(auth.JWTSecret()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "系统错误"})
		return
	}
	name := agent.DisplayName
	if name == "" {
		name = agent.Username
	}
	csrf := newCSRFToken()
	setAgentAuthCookies(c, signed, csrf, int(time.Until(expiration).Seconds()))
	c.JSON(http.StatusOK, gin.H{
		"token":      signed,
		"username":   agent.Username,
		"name":       name,
		"expires_at": expiration.Format(time.RFC3339),
		"csrf_token": csrf,
	})
}

func AgentLogout(c *gin.Context) {
	clearAgentAuthCookies(c)
	c.JSON(http.StatusOK, gin.H{"message": "已退出登录"})
}

func AgentMe(c *gin.Context) {
	id := agentUserID(c)
	agent, err := db.GetAgentUserByID(id)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "账号不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":                 agent.ID,
		"username":                agent.Username,
		"name":                    agent.DisplayName,
		"status":                  agent.Status,
		"allowed_plans":           agent.AllowedPlans,
		"webhook_url":             agent.WebhookURL,
		"ref_prefix":              agent.RefPrefix,
		"rate_limit_rpm":          agent.RateLimitRPM,
		"max_concurrent_recharge": agent.MaxConcurrentRecharge,
		"max_batch_items":         agent.MaxBatchItems,
		"unused_cdk_count":        agentUnusedCDKCount(agent.ID),
	})
}

func agentUnusedCDKCount(agentID int64) int {
	n, _ := db.CountAgentUnusedCDKs(agentID)
	return n
}

func AgentChangePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	id := agentUserID(c)
	agent, hash, err := db.GetAgentUserByUsername(c.GetString("username"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "账号不存在"})
		return
	}
	if agent.ID != id {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	ok, _ := db.VerifyAdminPassword(hash, strings.TrimSpace(req.OldPassword))
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "原密码错误"})
		return
	}
	newPass := strings.TrimSpace(req.NewPassword)
	if db.IsWeakPassword(newPass) || !hasLetterAndDigit(newPass) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码太弱（至少 12 位，含字母和数字）"})
		return
	}
	if err := db.ResetAgentUserPassword(id, newPass); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "修改失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "密码已更新"})
}

// ---- Settings ----

func AgentGetSettings(c *gin.Context) {
	agent, err := db.GetAgentUserByID(agentUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"webhook_url":             agent.WebhookURL,
		"ref_prefix":              agent.RefPrefix,
		"has_webhook_secret":      agent.HasWebhookKey,
		"rate_limit_rpm":          agent.RateLimitRPM,
		"max_concurrent_recharge": agent.MaxConcurrentRecharge,
		"max_batch_items":         agent.MaxBatchItems,
		"unused_cdk_count":        agentUnusedCDKCount(agent.ID),
	})
}

func AgentPutSettings(c *gin.Context) {
	var req struct {
		WebhookURL string `json:"webhook_url"`
		RefPrefix  string `json:"ref_prefix"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	// 回调地址由代理自由填写，等于让外部输入决定服务端往哪发请求；
	// 保存时先挡一道内网/保留地址，投递时还会按实际连接地址再判一次。
	if err := agentwebhook.ValidateURL(req.WebhookURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := agentUserID(c)
	if err := db.UpdateAgentUserSettings(id, req.WebhookURL, req.RefPrefix); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已保存"})
}

func AgentRotateWebhookSecret(c *gin.Context) {
	id := agentUserID(c)
	secret, err := db.RegenerateAgentWebhookSecret(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook_secret": secret, "message": "请妥善保存，仅展示一次"})
}

// ---- API Keys ----

func AgentListAPIKeys(c *gin.Context) {
	keys, err := db.ListAgentAPIKeys(agentUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": keys})
}

func AgentCreateAPIKey(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	_ = c.ShouldBindJSON(&req)
	plain := db.GenerateAgentAPIKeyPlain()
	key, err := db.CreateAgentAPIKey(agentUserID(c), req.Name, plain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"key":     key,
		"api_key": plain,
		"message": "请妥善保存 API Key，仅展示一次",
	})
}

func AgentRevokeAPIKey(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := db.RevokeAgentAPIKey(agentUserID(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已吊销"})
}

// ---- CDK Inventory ----

// AgentListCDKs GET /api/v1/agent/cdks
// 列出站长分配给本代理的卡密（含完整码），用于门户查看与复制，替代线下交接。
func AgentListCDKs(c *gin.Context) {
	agentID := agentUserID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	q := db.AgentCDKInventoryQuery{
		AgentUserID: agentID,
		Status:      c.Query("status"),
		Plan:        c.Query("plan"),
		Code:        c.Query("code"),
		Page:        page,
		PageSize:    pageSize,
	}
	list, total, err := db.ListAgentCDKInventory(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	summary, err := db.AgentCDKInventorySummaryFor(agentID)
	if err != nil {
		log.Printf("[agent] cdk inventory summary failed agent=%d: %v", agentID, err)
		summary = db.AgentCDKInventorySummary{}
	}
	c.JSON(http.StatusOK, gin.H{
		"list":      list,
		"total":     total,
		"page":      q.Page,
		"page_size": q.PageSize,
		"summary":   summary,
	})
}

// ---- Records ----

func AgentListRecords(c *gin.Context) {
	q := db.AgentRecordQuery{
		AgentUserID: agentUserID(c),
		Email:       c.Query("email"),
		CDK:         c.Query("cdk"),
		Status:      c.Query("status"),
		Plan:        c.Query("plan"),
	}
	// session 明文不走 query string（会进 access log / 浏览器历史），
	// 按 session 检索请用 POST /agent/records/search-session。
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	q.Page = page
	q.PageSize = pageSize

	list, total, err := db.ListAgentRechargeRecords(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"list":      list,
		"total":     total,
		"page":      q.Page,
		"page_size": q.PageSize,
	})
}

func AgentSearchRecordsBySession(c *gin.Context) {
	var req struct {
		Session  string `json:"session"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Session) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session required"})
		return
	}
	q := db.AgentRecordQuery{
		AgentUserID: agentUserID(c),
		SessionHash: HashSession(req.Session),
		Page:        req.Page,
		PageSize:    req.PageSize,
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = 20
	}
	list, total, err := db.ListAgentRechargeRecords(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": total, "page": q.Page, "page_size": q.PageSize})
}

// ---- Single recharge ----

type agentRechargeReq struct {
	Plan            string                  `json:"plan"`
	CDKCode         string                  `json:"cdk_code"`
	ClientReference string                  `json:"client_reference"`
	Account         batchRechargeCredential `json:"account"`
}

func AgentCreateRecharge(c *gin.Context) {
	var req agentRechargeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	agentID := agentUserID(c)
	agent, err := db.GetAgentUserByID(agentID)
	if err != nil || agent.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "账号不可用"})
		return
	}
	plan := strings.TrimSpace(req.Plan)
	if !agentPlanAllowed(agent, plan, agentSellableKeys(c.Request.Context())) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "套餐不可用", "error_code": "PLAN_NOT_AVAILABLE"})
		return
	}
	cdkCode := strings.TrimSpace(req.CDKCode)
	if cdkCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 cdk_code（须使用站长分配给你的卡密）", "error_code": "CDK_REQUIRED"})
		return
	}
	if err := db.CheckAgentCDKForRecharge(agentID, plan, cdkCode); err != nil {
		respondAgentCDKError(c, 0, err)
		return
	}
	cred := batchRechargeCredential{
		CDKCode:       cdkCode,
		Mode:          strings.ToLower(strings.TrimSpace(req.Account.Mode)),
		Session:       strings.TrimSpace(req.Account.Session),
		Email:         strings.TrimSpace(req.Account.Email),
		Password:      req.Account.Password,
		GptPassword:   strings.TrimSpace(req.Account.GptPassword),
		EmailPassword: strings.TrimSpace(req.Account.EmailPassword),
	}
	if cred.EmailPassword == "" {
		cred.EmailPassword = strings.TrimSpace(cred.Password)
	}
	if cred.Mode == "" {
		cred.Mode = "session"
	}
	if err := validateBatchCredential(cred); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	clientRef := strings.TrimSpace(req.ClientReference)
	if clientRef == "" {
		clientRef = strings.TrimSpace(agent.RefPrefix)
	}
	operator := fmt.Sprintf("agent:%d", agentID)
	source := "agent_api"
	if via, _ := c.Get("auth_via"); via == "jwt" {
		source = "agent_portal"
	}
	result, status, err := submitAgentRecharge(c, operator, agentID, source, plan, cred, clientRef)
	if err != nil {
		if status == 0 {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, result)
}

func AgentGetRecharge(c *gin.Context) {
	requestID := strings.TrimSpace(c.Param("request_id"))
	rec, err := db.GetAgentRechargeItem(agentUserID(c), requestID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, rec)
}

func AgentListPlans(c *gin.Context) {
	agent, err := db.GetAgentUserByID(agentUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// 卡台在线：用实时可售档位；卡台不可用：回落 plus / pro_5x / pro_20x 等常用套餐。
	// fee_usd 是该代理的有效售价（覆盖 → 全局默认 → 0），不是卡台服务费。
	catalog := resolveAgentPlanCatalog(c, agent)
	keys := make([]string, 0, len(catalog))
	for _, p := range catalog {
		keys = append(keys, p.Key)
	}
	effective, _, _, err := db.LoadAgentEffectivePlanPrices(agent.ID, keys)
	if err != nil {
		log.Printf("[agent] load plan prices failed agent=%d: %v", agent.ID, err)
		effective = db.AgentPlanPriceMap{}
	}
	out := make([]gin.H, 0, len(catalog))
	for _, p := range catalog {
		cents := effective[p.Key]
		row := gin.H{
			"key":             p.Key,
			"label":           p.Label,
			"price_cny_cents": cents,
			"price_yuan":      fmt.Sprintf("%.2f", float64(cents)/100),
			"is_credit":       p.IsCredit,
			"fulfillment":     db.PlanFulfillment(p.Key),
		}
		// 本站库存有限，先把可售数量给代理看，别让他下了单才知道不够。
		if db.IsLocalStockPlan(p.Key) {
			if stock, serr := db.CountUnassignedLocalStock(p.Key); serr == nil {
				row["stock"] = stock
			}
		}
		out = append(out, row)
	}
	payTypes := epay.ParsePayTypes(settingOr("epay_pay_types", "alipay"))
	cardReady := cardplatform.LoadConfig().APIKey != ""
	epayReady := loadEpayConfig().Ready()
	purchase := gin.H{
		"ready":               epayReady,
		"card_platform_ready": cardReady,
		"pay_types":           payTypes,
	}
	if !epayReady {
		purchase["reason"] = "易支付未配置，请联系站长"
	}
	c.JSON(http.StatusOK, gin.H{"plans": out, "purchase": purchase})
}

func submitAgentRecharge(c *gin.Context, operator string, agentID int64, source, plan string, cred batchRechargeCredential, clientRef string) (gin.H, int, error) {
	if cardplatform.LoadConfig().APIKey == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("卡台未配置")
	}
	if strings.TrimSpace(cred.CDKCode) == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("缺少 cdk_code")
	}
	cleaned := []batchRechargeCredential{cred}
	fp := batchFingerprint(operator, plan, cleaned)
	if existing, err := db.FindRecentAdminRechargeBatchByFingerprint(fp, batchRechargeDedupWindowSec); err == nil && existing != "" {
		crid := existing + "-001"
		if rec, err := db.GetAgentRechargeItem(agentID, crid); err == nil {
			return gin.H{
				"request_id": rec.RequestID,
				"batch_id":   rec.BatchID,
				"deduped":    true,
				"status":     rec.Status,
				"plan":       rec.Plan,
			}, http.StatusOK, nil
		}
	}
	agent, err := db.GetAgentUserByID(agentID)
	if err != nil {
		return nil, http.StatusForbidden, fmt.Errorf("账号不可用")
	}
	if agent.Status != "active" {
		return nil, http.StatusForbidden, fmt.Errorf("账号不可用")
	}
	inFlight, err := db.CountAgentInFlightRecharges(agentID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("系统繁忙，请稍后重试")
	}
	maxConc := agent.MaxConcurrentRecharge
	if maxConc <= 0 {
		maxConc = db.AgentDefaultMaxConcurrent
	}
	if inFlight >= maxConc {
		return nil, http.StatusTooManyRequests,
			fmt.Errorf("在途充值已达上限（%d 条），请等待当前任务完成", maxConc)
	}

	batchID := "agt-" + randomHex(8)
	crid := batchID + "-001"
	sessionHash := ""
	if cred.Mode == "session" && cred.Session != "" {
		sessionHash = HashSession(cred.Session)
	}
	emailPass := cred.EmailPassword
	if emailPass == "" {
		emailPass = cred.Password
	}
	item := db.AdminRechargeItem{
		BatchID:         batchID,
		Seq:             1,
		ClientRequestID: crid,
		ClientReference: clientRef,
		Plan:            plan,
		CredMode:        cred.Mode,
		AccountEmail:    cred.Email,
		GptPassword:     cred.GptPassword,
		EmailPassword:   emailPass,
		Session:         cred.Session,
		SessionHash:     sessionHash,
		CDKCode:         cred.CDKCode,
		Status:          itemStatusPending,
		Message:         "等待处理",
	}
	batch := db.AdminRechargeBatch{
		BatchID:     batchID,
		Operator:    operator,
		AgentUserID: agentID,
		Source:      source,
		Plan:        plan,
		Total:       1,
		Status:      batchStatusRunning,
	}
	if err := db.CreateAdminRechargeBatch(batch, fp, []db.AdminRechargeItem{item}); err != nil {
		log.Printf("[agent-recharge] create failed: %v", err)
		return nil, http.StatusInternalServerError, fmt.Errorf("创建任务失败")
	}
	jobs := []batchRechargeJob{{seq: 1, clientRequestID: crid, cred: cred}}
	go runBatchRecharge(batchID, plan, operator, jobs)
	return gin.H{
		"request_id":       crid,
		"batch_id":         batchID,
		"plan":             plan,
		"status":           batchStatusRunning,
		"client_reference": clientRef,
	}, http.StatusAccepted, nil
}

func validateBatchCredential(c batchRechargeCredential) error {
	switch c.Mode {
	case "session":
		if c.Session == "" {
			return fmt.Errorf("缺少 session")
		}
	case "mailbox":
		if !strings.Contains(c.Email, "@") || c.Password == "" {
			return fmt.Errorf("邮箱或密码无效")
		}
	default:
		return fmt.Errorf("mode 必须是 session 或 mailbox")
	}
	return nil
}

// ---- Admin: agent management ----

func AdminListAgents(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := db.ListAgentUsers(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stock, err := db.AgentCDKStockMap()
	if err != nil {
		log.Printf("[admin] agent cdk stock failed: %v", err)
		stock = map[int64]db.AgentCDKStock{}
	}
	out := make([]gin.H, 0, len(list))
	for _, u := range list {
		s := stock[u.ID]
		out = append(out, gin.H{
			"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
			"status": u.Status, "allowed_plans": u.AllowedPlans, "webhook_url": u.WebhookURL,
			"ref_prefix": u.RefPrefix, "rate_limit_rpm": u.RateLimitRPM,
			"max_concurrent_recharge": u.MaxConcurrentRecharge, "max_batch_items": u.MaxBatchItems,
			"created_at": u.CreatedAt, "updated_at": u.UpdatedAt,
			"has_webhook_secret": u.HasWebhookKey,
			"cdk_stock":          s,
		})
	}
	c.JSON(http.StatusOK, gin.H{"list": out})
}

func AdminCreateAgent(c *gin.Context) {
	var req struct {
		Username     string   `json:"username"`
		Password     string   `json:"password"`
		DisplayName  string   `json:"display_name"`
		AllowedPlans []string `json:"allowed_plans"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}
	if password == "" {
		password = db.RandomPassword(16)
	}
	if db.IsWeakPassword(password) || !hasLetterAndDigit(password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password too weak"})
		return
	}
	agent, err := db.CreateAgentUser(username, password, req.DisplayName, req.AllowedPlans)
	if err != nil {
		if errors.Is(err, db.ErrAgentUsernameTaken) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "用户名已存在，请换一个"})
			return
		}
		log.Printf("create agent failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}
	auditAdmin(c, "agent_create", "username="+username)
	c.JSON(http.StatusCreated, gin.H{
		"agent":    agent,
		"password": password,
		"message":  "请妥善保存初始密码，仅展示一次",
	})
}

func AdminUpdateAgentStatus(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	status := strings.TrimSpace(req.Status)
	if status != "active" && status != "suspended" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be active or suspended"})
		return
	}
	if err := db.UpdateAgentUserStatus(id, status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "agent_status", fmt.Sprintf("id=%d status=%s", id, status))
	c.JSON(http.StatusOK, gin.H{"message": "已更新"})
}

func AdminResetAgentPassword(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	password := db.RandomPassword(16)
	if err := db.ResetAgentUserPassword(id, password); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "agent_reset_password", fmt.Sprintf("id=%d", id))
	c.JSON(http.StatusOK, gin.H{"password": password, "message": "请妥善保存，仅展示一次"})
}

func AdminUpdateAgentPlans(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		AllowedPlans []string `json:"allowed_plans"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := db.UpdateAgentUserPlans(id, req.AllowedPlans); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已更新"})
}

func AdminUpdateAgentLimits(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		RateLimitRPM          int `json:"rate_limit_rpm"`
		MaxConcurrentRecharge int `json:"max_concurrent_recharge"`
		MaxBatchItems         int `json:"max_batch_items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.RateLimitRPM < 1 || req.RateLimitRPM > 600 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rate_limit_rpm 须在 1–600 之间"})
		return
	}
	if req.MaxConcurrentRecharge < 1 || req.MaxConcurrentRecharge > db.AgentMaxConcurrentHardCap {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("max_concurrent_recharge 须在 1–%d 之间", db.AgentMaxConcurrentHardCap),
		})
		return
	}
	if req.MaxBatchItems < 1 || req.MaxBatchItems > db.AgentMaxBatchItemsHardCap {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("max_batch_items 须在 1–%d 之间", db.AgentMaxBatchItemsHardCap),
		})
		return
	}
	// 单批条数不该超过在途额度，否则每一批都会被并发闸门直接拒掉。
	if req.MaxBatchItems > req.MaxConcurrentRecharge {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "max_batch_items 不能大于 max_concurrent_recharge，否则该代理提交任何整批都会被并发闸门拒绝",
		})
		return
	}
	if err := db.UpdateAgentUserLimits(id, req.RateLimitRPM, req.MaxConcurrentRecharge, req.MaxBatchItems); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "agent_limits", fmt.Sprintf("id=%d rpm=%d concurrent=%d batch=%d",
		id, req.RateLimitRPM, req.MaxConcurrentRecharge, req.MaxBatchItems))
	c.JSON(http.StatusOK, gin.H{"message": "已更新"})
}

// AdminAssignAgentCDKs POST /api/v1/admin/agents/:id/assign-cdks
// 站长在卡台发码/入库后，把卡密划给指定代理（线下拿货后录入）。
func AdminAssignAgentCDKs(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := db.GetAgentUserByID(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "代理不存在"})
		return
	}
	var req struct {
		Codes []string `json:"codes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if len(req.Codes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "codes 不能为空"})
		return
	}
	assigned, skipped, err := db.AssignCDKsToAgent(id, req.Codes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "agent_assign_cdks", fmt.Sprintf("id=%d assigned=%d skipped=%d", id, assigned, len(skipped)))
	c.JSON(http.StatusOK, gin.H{
		"assigned": assigned,
		"skipped":  skipped,
		"message":  fmt.Sprintf("已分配 %d 张卡密", assigned),
	})
}

// AdminUnassignAgentCDKs POST /api/v1/admin/agents/:id/unassign-cdks
// codes 为空 = 收回该代理全部未使用卡密（发错货时用）。
func AdminUnassignAgentCDKs(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := db.GetAgentUserByID(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "代理不存在"})
		return
	}
	var req struct {
		Codes []string `json:"codes"`
	}
	_ = c.ShouldBindJSON(&req)

	released, skipped, err := db.UnassignCDKsFromAgent(id, req.Codes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "agent_unassign_cdks", fmt.Sprintf("id=%d released=%d skipped=%d", id, released, len(skipped)))
	c.JSON(http.StatusOK, gin.H{
		"released": released,
		"skipped":  skipped,
		"message":  fmt.Sprintf("已收回 %d 张卡密", released),
	})
}

// AdminListAgentRecords GET /api/v1/admin/agents/:id/records
// 管理端按代理查订单明细，复用代理门户同一套查询（含 session 哈希检索）。
func AdminListAgentRecords(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	agent, err := db.GetAgentUserByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "代理不存在"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	q := db.AgentRecordQuery{
		AgentUserID: id,
		Email:       c.Query("email"),
		CDK:         c.Query("cdk"),
		Status:      c.Query("status"),
		Plan:        c.Query("plan"),
		Page:        page,
		PageSize:    pageSize,
	}
	list, total, err := db.ListAgentRechargeRecords(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"agent":     gin.H{"id": agent.ID, "username": agent.Username, "display_name": agent.DisplayName},
		"list":      list,
		"total":     total,
		"page":      q.Page,
		"page_size": q.PageSize,
	})
}

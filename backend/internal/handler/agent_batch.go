package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/xlsxsheet"
	"github.com/gin-gonic/gin"
)

// 代理批量充值。执行链路与管理端完全一致（复用 runBatchRecharge），
// 差别只在鉴权、归属隔离和额度控制。

// agentIdempotencyWindowSec 显式 Idempotency-Key 的复用窗口。
// 不带 key 时退回 batchRechargeDedupWindowSec（60 秒）的误重复提交保护。
const agentIdempotencyWindowSec = 24 * 3600

type agentBatchItemReq struct {
	CDKCode         string `json:"cdk_code"`
	Mode            string `json:"mode"`
	Session         string `json:"session"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	GptPassword     string `json:"gpt_password"`
	EmailPassword   string `json:"email_password"`
	ClientReference string `json:"client_reference"`
}

type agentBatchCreateReq struct {
	Plan  string              `json:"plan"`
	Items []agentBatchItemReq `json:"items"`
}

// AgentBatchCreate POST /api/v1/agent/batch-recharge
func AgentBatchCreate(c *gin.Context) {
	var req agentBatchCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		agentErr(c, http.StatusBadRequest, "INVALID_REQUEST", "请求体不是合法 JSON")
		return
	}
	agentID := agentUserID(c)
	agent, err := db.GetAgentUserByID(agentID)
	if err != nil || agent.Status != "active" {
		agentErr(c, http.StatusForbidden, "AGENT_INACTIVE", "账号不可用")
		return
	}
	plan := strings.TrimSpace(req.Plan)
	if !agentPlanAllowed(agent, plan, agentSellableKeys(c.Request.Context())) {
		agentErr(c, http.StatusBadRequest, "PLAN_NOT_AVAILABLE", "套餐不可用")
		return
	}

	maxItems := agent.MaxBatchItems
	if maxItems <= 0 {
		maxItems = db.AgentDefaultMaxBatchItems
	}
	if len(req.Items) == 0 {
		agentErr(c, http.StatusBadRequest, "ITEMS_REQUIRED", "items 不能为空")
		return
	}
	if len(req.Items) > maxItems {
		agentErr(c, http.StatusBadRequest, "BATCH_TOO_LARGE",
			fmt.Sprintf("单批最多 %d 条，请分批提交", maxItems))
		return
	}

	// 逐条规范化 + 校验。错误只报序号，不回显任何凭据内容。
	creds := make([]batchRechargeCredential, 0, len(req.Items))
	refs := make([]string, 0, len(req.Items))
	seenCDK := map[string]int{}
	defaultRef := strings.TrimSpace(agent.RefPrefix)
	for i, raw := range req.Items {
		cdkCode := strings.TrimSpace(raw.CDKCode)
		if cdkCode == "" {
			agentErr(c, http.StatusBadRequest, "CDK_REQUIRED",
				fmt.Sprintf("第 %d 条：缺少 cdk_code（须使用站长分配给你的卡密）", i+1))
			return
		}
		if prev, dup := seenCDK[strings.ToUpper(cdkCode)]; dup {
			agentErr(c, http.StatusBadRequest, "CDK_DUPLICATE",
				fmt.Sprintf("第 %d 条与第 %d 条卡密重复", i+1, prev))
			return
		}
		seenCDK[strings.ToUpper(cdkCode)] = i + 1
		if err := db.CheckAgentCDKForRecharge(agentID, plan, cdkCode); err != nil {
			respondAgentCDKError(c, i+1, err)
			return
		}
		cred := batchRechargeCredential{
			CDKCode:       cdkCode,
			Mode:          strings.ToLower(strings.TrimSpace(raw.Mode)),
			Session:       strings.TrimSpace(raw.Session),
			Email:         strings.TrimSpace(raw.Email),
			Password:      raw.Password,
			GptPassword:   strings.TrimSpace(raw.GptPassword),
			EmailPassword: strings.TrimSpace(raw.EmailPassword),
		}
		if cred.Mode == "" {
			cred.Mode = "session"
		}
		if cred.EmailPassword == "" {
			cred.EmailPassword = strings.TrimSpace(cred.Password)
		}
		if err := validateBatchCredential(cred); err != nil {
			agentErr(c, http.StatusBadRequest, "INVALID_ITEM",
				fmt.Sprintf("第 %d 条：%s", i+1, err.Error()))
			return
		}
		ref := strings.TrimSpace(raw.ClientReference)
		if ref == "" {
			ref = defaultRef
		}
		creds = append(creds, cred)
		refs = append(refs, ref)
	}

	// 幂等：带 Idempotency-Key 时按 key 去重（24 小时），否则按凭据指纹兜误重复提交（60 秒）。
	// 两种情况都复用 admin_recharge_batches.fingerprint 这一列。
	operator := fmt.Sprintf("agent:%d", agentID)
	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	fp := batchFingerprint(operator, plan, creds)
	window := batchRechargeDedupWindowSec
	if idemKey != "" {
		sum := sha256.Sum256([]byte(fmt.Sprintf("idem|%d|%s", agentID, idemKey)))
		fp = hex.EncodeToString(sum[:])
		window = agentIdempotencyWindowSec
	}
	if existing, err := db.FindRecentAdminRechargeBatchByFingerprint(fp, window); err == nil && existing != "" {
		items, _ := db.ListAgentRechargeBatchItems(agentID, existing)
		c.JSON(http.StatusOK, gin.H{
			"batch_id": existing,
			"deduped":  true,
			"message":  "检测到重复提交，已返回原批次，未重复扣款",
			"total":    len(items),
			"plan":     plan,
			"items":    agentBatchItemRefs(items),
		})
		return
	}

	// 并发闸门：单条与批量共用「在途明细条数」额度。
	inFlight, err := db.CountAgentInFlightRecharges(agentID)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "INTERNAL", "系统繁忙，请稍后重试")
		return
	}
	maxConc := agent.MaxConcurrentRecharge
	if maxConc <= 0 {
		maxConc = db.AgentDefaultMaxConcurrent
	}
	if inFlight+len(req.Items) > maxConc {
		agentErr(c, http.StatusTooManyRequests, "CONCURRENCY_LIMIT",
			fmt.Sprintf("在途充值 %d 条，本批 %d 条，超出并发上限 %d，请等待当前任务完成",
				inFlight, len(req.Items), maxConc))
		return
	}

	if cardplatform.LoadConfig().APIKey == "" {
		agentErr(c, http.StatusServiceUnavailable, "CARD_PLATFORM_UNCONFIGURED", "卡台未配置，请联系管理员")
		return
	}

	batchID := "agb-" + randomHex(8)
	items := make([]db.AdminRechargeItem, 0, len(creds))
	jobs := make([]batchRechargeJob, 0, len(creds))
	for i, cred := range creds {
		crid := fmt.Sprintf("%s-%03d", batchID, i+1)
		sessionHash := ""
		if cred.Mode == "session" && cred.Session != "" {
			sessionHash = HashSession(cred.Session)
		}
		items = append(items, db.AdminRechargeItem{
			BatchID:         batchID,
			Seq:             i + 1,
			ClientRequestID: crid,
			ClientReference: refs[i],
			Plan:            plan,
			CredMode:        cred.Mode,
			AccountEmail:    cred.Email,
			GptPassword:     cred.GptPassword,
			EmailPassword:   cred.EmailPassword,
			Session:         cred.Session,
			SessionHash:     sessionHash,
			CDKCode:         cred.CDKCode,
			Status:          itemStatusPending,
			Message:         "等待处理",
		})
		jobs = append(jobs, batchRechargeJob{seq: i + 1, clientRequestID: crid, cred: cred})
	}

	source := "agent_api"
	if via, _ := c.Get("auth_via"); via == "jwt" {
		source = "agent_portal"
	}
	batch := db.AdminRechargeBatch{
		BatchID:     batchID,
		Operator:    operator,
		AgentUserID: agentID,
		Source:      source,
		Plan:        plan,
		Total:       len(items),
		Status:      batchStatusRunning,
	}
	if err := db.CreateAdminRechargeBatch(batch, fp, items); err != nil {
		log.Printf("[agent-batch] create batch failed: %v", err)
		agentErr(c, http.StatusInternalServerError, "INTERNAL", "创建批次失败")
		return
	}

	go runBatchRecharge(batchID, plan, operator, jobs)

	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"seq":              it.Seq,
			"request_id":       it.ClientRequestID,
			"client_reference": it.ClientReference,
		})
	}
	c.JSON(http.StatusAccepted, gin.H{
		"batch_id": batchID,
		"total":    len(items),
		"plan":     plan,
		"status":   batchStatusRunning,
		"items":    out,
	})
}

// AgentBatchList GET /api/v1/agent/batch-recharge
func AgentBatchList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := db.ListAgentRechargeBatches(agentUserID(c), page, pageSize)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": total, "page": page, "page_size": pageSize})
}

// AgentBatchDetail GET /api/v1/agent/batch-recharge/:batch_id
func AgentBatchDetail(c *gin.Context) {
	agentID := agentUserID(c)
	batchID := strings.TrimSpace(c.Param("batch_id"))
	batch, err := db.GetAgentRechargeBatch(agentID, batchID)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	if batch == nil {
		agentErr(c, http.StatusNotFound, "BATCH_NOT_FOUND", "批次不存在")
		return
	}
	items, err := db.ListAgentRechargeBatchItems(agentID, batchID)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"batch": batch, "items": items})
}

// AgentBatchExport GET /api/v1/agent/batch-recharge/:batch_id/export?scope=all|success|failed
//
// 导出的是对账结果而非凭据：凭据本就是代理自己提交的，回吐一份只会多一个泄露面。
func AgentBatchExport(c *gin.Context) {
	agentID := agentUserID(c)
	batchID := strings.TrimSpace(c.Param("batch_id"))
	scope := strings.ToLower(strings.TrimSpace(c.DefaultQuery("scope", "all")))
	switch scope {
	case "all", "success", "failed":
	default:
		agentErr(c, http.StatusBadRequest, "INVALID_SCOPE", "scope 只能是 all | success | failed")
		return
	}
	batch, err := db.GetAgentRechargeBatch(agentID, batchID)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	if batch == nil {
		agentErr(c, http.StatusNotFound, "BATCH_NOT_FOUND", "批次不存在")
		return
	}
	items, err := db.ListAgentRechargeBatchItems(agentID, batchID)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		switch scope {
		case "success":
			if it.Status != itemStatusSuccess {
				continue
			}
		case "failed":
			if it.Status != itemStatusFailed && it.Status != itemStatusSkipped {
				continue
			}
		}
		rows = append(rows, []string{
			strconv.Itoa(it.Seq), it.RequestID, it.ClientReference, it.Plan,
			it.AccountEmail, it.Status, it.Message, it.UpstreamOrderID, it.UpdatedAt,
		})
	}
	header := []string{"序号", "request_id", "业务单号", "套餐", "账号邮箱", "状态", "说明", "上游订单号", "更新时间"}
	var buf bytes.Buffer
	if err := xlsxsheet.Write(&buf, "对账", header, rows); err != nil {
		log.Printf("[agent-batch] export xlsx failed batch=%s: %v", batchID, err)
		agentErr(c, http.StatusInternalServerError, "INTERNAL", "生成 Excel 失败")
		return
	}
	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="batch-%s-%s.xlsx"`, batchID, scope))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

// ---- Webhook 投递日志 ----

// AgentListWebhookDeliveries GET /api/v1/agent/webhooks/deliveries
func AgentListWebhookDeliveries(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := db.ListAgentWebhookDeliveries(agentUserID(c), page, pageSize)
	if err != nil {
		agentErr(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": total, "page": page, "page_size": pageSize})
}

// AgentRetryWebhookDelivery POST /api/v1/agent/webhooks/deliveries/:id/retry
func AgentRetryWebhookDelivery(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		agentErr(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid id")
		return
	}
	if err := db.RequeueAgentWebhook(agentUserID(c), id); err != nil {
		agentErr(c, http.StatusNotFound, "DELIVERY_NOT_FOUND", "记录不存在或当前状态不可重投")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已重新入队，稍后自动投递"})
}

func agentBatchItemRefs(items []db.AgentRechargeRecord) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"seq":              it.Seq,
			"request_id":       it.RequestID,
			"client_reference": it.ClientReference,
		})
	}
	return out
}

// agentErr 统一错误体，代理侧按 error_code 做程序判断，error 只用于展示。
func agentErr(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": msg, "error_code": code})
}

func respondAgentCDKError(c *gin.Context, itemIndex int, err error) {
	prefix := ""
	if itemIndex > 0 {
		prefix = fmt.Sprintf("第 %d 条：", itemIndex)
	}
	status := http.StatusBadRequest
	code := db.AgentCDKErrorCode(err)
	if code == "CDK_WRONG_AGENT" {
		status = http.StatusForbidden
	} else if code == "CDK_IN_FLIGHT" {
		status = http.StatusConflict
	}
	agentErr(c, status, code, prefix+db.AgentCDKErrorMessage(err))
}

// AgentValidateCDKs POST /api/v1/agent/cdk/validate
// 批量预检卡密：去重、校验归属/套餐/状态，不预留。通过后再填凭据提交批次。
func AgentValidateCDKs(c *gin.Context) {
	var req struct {
		Plan  string   `json:"plan"`
		Codes []string `json:"codes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		agentErr(c, http.StatusBadRequest, "INVALID_REQUEST", "请求体不是合法 JSON")
		return
	}
	agentID := agentUserID(c)
	agent, err := db.GetAgentUserByID(agentID)
	if err != nil || agent.Status != "active" {
		agentErr(c, http.StatusForbidden, "AGENT_INACTIVE", "账号不可用")
		return
	}
	plan := strings.TrimSpace(req.Plan)
	if plan == "" {
		agentErr(c, http.StatusBadRequest, "PLAN_REQUIRED", "请选择套餐")
		return
	}
	if !agentPlanAllowed(agent, plan, agentSellableKeys(c.Request.Context())) {
		agentErr(c, http.StatusBadRequest, "PLAN_NOT_AVAILABLE", "套餐不可用")
		return
	}
	if len(req.Codes) == 0 {
		agentErr(c, http.StatusBadRequest, "CODES_REQUIRED", "请粘贴至少一张卡密")
		return
	}
	maxItems := agent.MaxBatchItems
	if maxItems <= 0 {
		maxItems = db.AgentDefaultMaxBatchItems
	}
	summary := db.ValidateAgentCDKBatch(agentID, plan, req.Codes)
	c.JSON(http.StatusOK, gin.H{
		"plan":             plan,
		"summary":          summary,
		"warn_batch_limit": summary.ValidCount > maxItems,
		"max_batch_items":  maxItems,
	})
}

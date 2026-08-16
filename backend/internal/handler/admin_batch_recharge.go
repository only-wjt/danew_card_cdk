package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// 管理员批量充值（方案 A）：管理端界面上不出现卡密，后端在一个批次里
// 自动完成「发码 → preview → preflight → redeem → 轮询 result」。
// 卡密退化为不可见的内部记账凭证，平台侧的自动开卡/资金上限能力继续复用。
//
// 资金链路与用户端 /batch 完全一致：服务费在发码时扣，上游订阅实付由本账户承担。
// 参考：docs/danew-cdk-zh.md、docs/danew-openapi-zh.md §6.16

const (
	// 单批上限；再大就该拆批，避免一次性把余额打光。
	batchRechargeMaxItems = 100
	// 卡台发码单次上限 50（docs/danew-openapi-zh.md:932）。
	// 这里只作为调度分片大小：每片之间检查中止条件，片内仍逐条 count=1 发码，
	// 这样即使发码请求超时，最多只丢一张码的服务费（完整码只在响应返回一次）。
	batchRechargeIssueChunk = 50
	// 提交阶段并发度。卡台有限流，别开太大。
	batchRechargeConcurrency = 4
	// 误双击去重窗口。
	batchRechargeDedupWindowSec = 60

	batchRechargePollInterval = 3 * time.Second
	batchRechargePollTimeout  = 8 * time.Minute
	// 单条上游调用超时上限（cardplatform.Client 自带 45s，这里再加总闸）。
	batchRechargeItemTimeout = 3 * time.Minute
)

// 明细状态机
const (
	itemStatusPending   = "pending"
	itemStatusIssuing   = "issuing"
	itemStatusPreparing = "preparing"
	itemStatusSubmitted = "submitted"
	itemStatusRunning   = "processing"
	itemStatusSuccess   = "success"
	itemStatusFailed    = "failed"
	itemStatusSkipped   = "skipped"
	// unknown：redeem 结果不确定（超时/网络断）。绝不重试，只能轮询确认。
	// docs/danew-cdk-zh.md:72「结果不确定或 review 状态时严禁重复提交」
	itemStatusUnknown = "unknown"
)

const (
	batchStatusRunning = "running"
	batchStatusDone    = "done"
	batchStatusPaused  = "paused"
)

var batchRechargePlanFallbackFeeMinor = map[string]int64{
	"plus":    100,
	"pro_5x":  500,
	"pro_20x": 1000,
}

// batchRechargeCredential 单条 ChatGPT 账号凭据。只在内存中流转，绝不落库、绝不进日志。
type batchRechargeCredential struct {
	Mode     string `json:"mode"` // session | mailbox
	Session  string `json:"session"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type batchRechargeCreateReq struct {
	Plan             string                    `json:"plan"`
	FundingConfirmed bool                      `json:"funding_confirmed"`
	Items            []batchRechargeCredential `json:"items"`
}

// batchRechargeJob 执行期的一条任务（凭据只存在于 goroutine 内存）。
type batchRechargeJob struct {
	seq             int
	clientRequestID string
	cred            batchRechargeCredential
	// 提交阶段结束后填充，供轮询阶段使用
	redemptionToken string
	terminal        bool
}

// ---- 工具 ----

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// credIdentity 返回一条凭据的稳定标识，用于指纹计算。
// session 只取哈希前缀，绝不使用明文。
func credIdentity(c batchRechargeCredential) string {
	if c.Mode == "mailbox" {
		return "mailbox:" + strings.ToLower(strings.TrimSpace(c.Email))
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(c.Session)))
	return "session:" + hex.EncodeToString(sum[:])[:16]
}

// batchFingerprint 同一操作员 + 同一套餐 + 同一批凭据 → 同一指纹，用于 60 秒内去重。
func batchFingerprint(operator, plan string, items []batchRechargeCredential) string {
	h := sha256.New()
	h.Write([]byte(operator + "|" + plan))
	for _, it := range items {
		h.Write([]byte("|" + credIdentity(it)))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func strPtr(s string) *string { return &s }

// ---- 上游响应解析（公开 CDK 接口不是 code/msg envelope，字段位置比较松散）----

func jsonMap(raw json.RawMessage) map[string]any {
	var m map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// pickNested 在 root、root.data、root.order、root.data.order 里找第一个非空字符串。
// 按 key 优先（而非 scope 优先）：调用方传的 key 顺序即语义优先级，
// 这样 pickNested(m,"order_id","id") 不会被 root 上无关的 id 抢先命中。
func pickNested(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	scopes := []map[string]any{m}
	for _, nest := range []string{"data", "order"} {
		if inner, ok := m[nest].(map[string]any); ok {
			scopes = append(scopes, inner)
			if deeper, ok := inner["order"].(map[string]any); ok {
				scopes = append(scopes, deeper)
			}
		}
	}
	for _, k := range keys {
		for _, sc := range scopes {
			if v := str(sc[k]); v != "" {
				return v
			}
		}
	}
	return ""
}

func upstreamMessage(m map[string]any) string {
	return pickNested(m, "message", "user_message", "msg", "error")
}

// 终态判定，与 docs/danew-cdk-zh.md:74-83 的状态表对齐。
func isTerminalOrderStatus(st string) bool {
	switch strings.ToLower(strings.TrimSpace(st)) {
	case "completed", "declined", "failed_precharge", "cancelled", "failed":
		return true
	}
	return false
}

func isSuccessOrderStatus(st string) bool {
	return strings.EqualFold(strings.TrimSpace(st), "completed")
}

// errorCodeOf 从上游错误里取 error_code（GPT 直充错误处理见 openapi 文档 §6.16.7）。
func errorCodeOf(err error) string {
	if ae, ok := err.(*cardplatform.APIError); ok {
		return strings.ToUpper(strings.TrimSpace(ae.ErrorCode))
	}
	return ""
}

func errorTextOf(err error) string {
	if ae, ok := err.(*cardplatform.APIError); ok {
		if strings.TrimSpace(ae.Msg) != "" {
			return ae.Msg
		}
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func nonEmptyMsg(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// ---- HTTP handlers ----

// AdminBatchRechargeCreate POST /api/v1/admin/cardplatform/batch-recharge
// 受理一批充值任务，立即返回 batch_id，实际执行在后台 goroutine。
func AdminBatchRechargeCreate(c *gin.Context) {
	var req batchRechargeCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	plan := strings.TrimSpace(req.Plan)
	switch plan {
	case "plus", "pro_5x", "pro_20x":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan must be plus | pro_5x | pro_20x"})
		return
	}
	if !req.FundingConfirmed {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "funding_confirmed must be true（确认承担开卡/充值/订阅实付与服务费）",
		})
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items required"})
		return
	}
	if len(req.Items) > batchRechargeMaxItems {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("单批最多 %d 条，请分批提交", batchRechargeMaxItems),
		})
		return
	}

	// 逐条规范化 + 校验，错误里只报序号不回显凭据
	cleaned := make([]batchRechargeCredential, 0, len(req.Items))
	for i, raw := range req.Items {
		it := batchRechargeCredential{
			Mode:     strings.ToLower(strings.TrimSpace(raw.Mode)),
			Session:  strings.TrimSpace(raw.Session),
			Email:    strings.TrimSpace(raw.Email),
			Password: raw.Password,
		}
		if it.Mode == "" {
			it.Mode = "session"
		}
		switch it.Mode {
		case "session":
			if it.Session == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("第 %d 条缺少 session", i+1)})
				return
			}
		case "mailbox":
			if !strings.Contains(it.Email, "@") || it.Password == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("第 %d 条邮箱或密码无效", i+1)})
				return
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("第 %d 条 mode 必须是 session 或 mailbox", i+1)})
			return
		}
		cleaned = append(cleaned, it)
	}

	uname, _ := c.Get("username")
	operator, _ := uname.(string)

	// 幂等锁 3/3：60 秒内同操作员 + 同套餐 + 同一批凭据 → 直接返回原批次
	fp := batchFingerprint(operator, plan, cleaned)
	if existing, err := db.FindRecentAdminRechargeBatchByFingerprint(fp, batchRechargeDedupWindowSec); err == nil && existing != "" {
		c.JSON(http.StatusOK, gin.H{
			"batch_id": existing,
			"deduped":  true,
			"message":  "检测到 60 秒内的相同提交，已返回原批次，未重复扣款",
			"total":    len(cleaned),
			"plan":     plan,
		})
		return
	}

	if cardplatform.LoadConfig().APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "card_api_key not configured，请先在「卡台接入」配置"})
		return
	}
	cli := cardplatform.NewFromSettings()

	// 余额预检：服务费在发码时即时扣款，必须用 spendable_balance
	// （20U 风险保证金含在 balance 里但不可消费，docs/danew-openapi-zh.md:29）
	feeMinor := batchRechargeServiceFeeMinor(c.Request.Context(), cli, plan)
	needUSD := float64(feeMinor) * float64(len(cleaned)) / 100.0
	bal, balErr := cli.GetBalance(c.Request.Context())
	if balErr != nil {
		writeCardErr(c, balErr)
		return
	}
	spendable, _ := bal.SpendableBalance.Float64()
	if spendable < needUSD {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("卡台可消费余额不足：需要约 $%.2f 服务费，当前可消费 $%.2f（另需承担上游订阅实付）",
				needUSD, spendable),
			"error_code":        "insufficient_balance",
			"need_usd":          needUSD,
			"spendable_balance": spendable,
		})
		return
	}

	batchID := "brc-" + randomHex(8)
	items := make([]db.AdminRechargeItem, 0, len(cleaned))
	jobs := make([]batchRechargeJob, 0, len(cleaned))
	for i, cred := range cleaned {
		// 幂等锁 1/3：client_request_id 后端生成并落库唯一索引；
		// 卡台侧同一 client_request_id 返回原订单（docs/danew-openapi-zh.md:904）
		crid := fmt.Sprintf("%s-%03d", batchID, i+1)
		email := ""
		if cred.Mode == "mailbox" {
			email = cred.Email
		}
		items = append(items, db.AdminRechargeItem{
			BatchID:         batchID,
			Seq:             i + 1,
			ClientRequestID: crid,
			Plan:            plan,
			CredMode:        cred.Mode,
			AccountEmail:    email,
			Status:          itemStatusPending,
			Message:         "等待处理",
		})
		jobs = append(jobs, batchRechargeJob{seq: i + 1, clientRequestID: crid, cred: cred})
	}

	batch := db.AdminRechargeBatch{
		BatchID:  batchID,
		Operator: operator,
		Plan:     plan,
		Total:    len(items),
		Status:   batchStatusRunning,
	}
	if err := db.CreateAdminRechargeBatch(batch, fp, items); err != nil {
		log.Printf("[batch-recharge] create batch failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建批次失败"})
		return
	}

	// 审计：只记聚合信息，不含 session / 邮箱密码 / 完整 CDK 码
	auditAdmin(c, "admin_batch_recharge_create",
		fmt.Sprintf("batch=%s plan=%s count=%d fee_usd=%.2f", batchID, plan, len(items), needUSD))

	go runBatchRecharge(batchID, plan, operator, jobs)

	c.JSON(http.StatusAccepted, gin.H{
		"batch_id":          batchID,
		"total":             len(items),
		"plan":              plan,
		"estimated_fee_usd": needUSD,
		"status":            batchStatusRunning,
	})
}

// batchRechargeServiceFeeMinor 取套餐服务费（美分）。拉不到实时价就退回文档默认价。
func batchRechargeServiceFeeMinor(ctx context.Context, cli *cardplatform.Client, plan string) int64 {
	if plans, err := cli.GetPlans(ctx); err == nil && plans != nil {
		if p, ok := plans.Plans[plan]; ok && p.ServiceFeeUsdMinor > 0 {
			return p.ServiceFeeUsdMinor
		}
	}
	if v, ok := batchRechargePlanFallbackFeeMinor[plan]; ok {
		return v
	}
	return 0
}

// AdminBatchRechargeDetail GET /api/v1/admin/cardplatform/batch-recharge/:batch_id
func AdminBatchRechargeDetail(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("batch_id"))
	if batchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "batch_id required"})
		return
	}
	batch, err := db.GetAdminRechargeBatch(batchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if batch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "批次不存在"})
		return
	}
	items, err := db.ListAdminRechargeItems(batchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = []db.AdminRechargeItem{}
	}
	c.JSON(http.StatusOK, gin.H{
		"batch": batch,
		"items": items,
		"stats": batchRechargeStats(items),
	})
}

// AdminBatchRechargeRetry POST /api/v1/admin/cardplatform/batch-recharge/:batch_id/retry
//
// 这里的「重试」只做一件事：把还没落终态的明细重新向上游查一次结果并对齐。
// 它不会重新发起充值——凭据只存在于原批次的 goroutine 内存里，进程重启或批次
// 结束后就没了，服务端无从重放。真正失败的条目必须由前端持原始 Excel 数据
// 重新提交成一个新批次，响应里的 resubmit 列表就是给前端用的。
func AdminBatchRechargeRetry(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("batch_id"))
	if batchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "batch_id required"})
		return
	}
	batch, err := db.GetAdminRechargeBatch(batchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if batch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "批次不存在"})
		return
	}
	items, err := db.ListAdminRechargeItems(batchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cli := cardplatform.NewFromSettings()
	reconciled := 0
	for _, it := range items {
		if it.RedemptionToken == "" {
			continue
		}
		switch it.Status {
		case itemStatusUnknown, itemStatusRunning, itemStatusSubmitted, itemStatusPreparing:
			j := batchRechargeJob{
				seq:             it.Seq,
				clientRequestID: it.ClientRequestID,
				redemptionToken: it.RedemptionToken,
			}
			if resolveOneRecharge(cli, &j) {
				reconciled++
			}
		}
	}

	items, _ = db.ListAdminRechargeItems(batchID)
	// resubmit：需要前端拿原始凭据重新提交的条目（不含任何敏感字段）
	resubmit := make([]gin.H, 0)
	for _, it := range items {
		if it.Status == itemStatusFailed || it.Status == itemStatusSkipped {
			resubmit = append(resubmit, gin.H{
				"seq": it.Seq, "account_email": it.AccountEmail,
				"cred_mode": it.CredMode, "status": it.Status, "message": it.Message,
			})
		}
	}
	st := batchRechargeStats(items)
	allDone := true
	for _, it := range items {
		switch it.Status {
		case itemStatusSuccess, itemStatusFailed, itemStatusSkipped, itemStatusUnknown:
		default:
			allDone = false
		}
	}
	if allDone && batch.Status == batchStatusRunning {
		_ = db.SetAdminRechargeBatchStatus(batchID, batchStatusDone, "已与上游对齐")
	}

	auditAdmin(c, "admin_batch_recharge_retry",
		fmt.Sprintf("batch=%s reconciled=%d resubmit=%d", batchID, reconciled, len(resubmit)))

	c.JSON(http.StatusOK, gin.H{
		"batch_id":   batchID,
		"reconciled": reconciled,
		"stats":      st,
		"resubmit":   resubmit,
		"note":       "服务端只做上游状态对齐，不会重复扣款；失败条目需前端持原数据重新提交为新批次",
	})
}

// AdminBatchRechargeList GET /api/v1/admin/cardplatform/batch-recharge
func AdminBatchRechargeList(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	batches, err := db.ListAdminRechargeBatches(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": batches, "total": len(batches)})
}

func batchRechargeStats(items []db.AdminRechargeItem) gin.H {
	var total, success, failed, running, pending, unknown int
	for _, it := range items {
		total++
		switch it.Status {
		case itemStatusSuccess:
			success++
		case itemStatusFailed, itemStatusSkipped:
			failed++
		case itemStatusUnknown:
			unknown++
		case itemStatusPending:
			pending++
		default:
			running++
		}
	}
	return gin.H{
		"total": total, "success": success, "failed": failed,
		"running": running, "pending": pending, "unknown": unknown,
	}
}

// ---- executor ----

// runBatchRecharge 两阶段执行：
//
//	阶段一（并发）：发码 → preview → preflight → redeem，拿到受理结果就返回，不等开通
//	阶段二（单协程轮询）：对未终态的明细统一轮询 result，直到终态或超时
//
// 凭据只存在于本函数的内存中，批次结束即释放；失败项无法在服务端重试，
// 需要前端持原始数据重新提交为新批次。
func runBatchRecharge(batchID, plan, operator string, jobs []batchRechargeJob) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[batch-recharge] panic batch=%s: %v", batchID, r)
			_ = db.SetAdminRechargeBatchStatus(batchID, batchStatusDone, "执行异常中断")
		}
	}()

	cli := cardplatform.NewFromSettings()
	// aborted：余额耗尽等致命错误，停止后续条目，避免继续烧钱
	var aborted atomic.Bool
	var abortMsg atomic.Value

	for start := 0; start < len(jobs); start += batchRechargeIssueChunk {
		if aborted.Load() {
			break
		}
		end := start + batchRechargeIssueChunk
		if end > len(jobs) {
			end = len(jobs)
		}
		chunk := jobs[start:end]

		var wg sync.WaitGroup
		sem := make(chan struct{}, batchRechargeConcurrency)
		for i := range chunk {
			job := &chunk[i]
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if aborted.Load() {
					patchItem(job.clientRequestID, itemStatusFailed, "批次已中止，未提交")
					return
				}
				if fatal, msg := submitOneRecharge(cli, plan, job); fatal {
					if aborted.CompareAndSwap(false, true) {
						abortMsg.Store(msg)
					}
				}
			}()
		}
		wg.Wait()
	}

	// 中止后剩余分片根本没被调度，这些条目还停在 pending，需要落一个明确状态
	if aborted.Load() {
		for _, it := range mustListItems(batchID) {
			if it.Status == itemStatusPending {
				patchItem(it.ClientRequestID, itemStatusFailed, "批次已中止，未提交")
			}
		}
	}

	// 阶段二：轮询未终态
	pollBatchRecharge(cli, jobs)

	items, _ := db.ListAdminRechargeItems(batchID)
	st := batchRechargeStats(items)
	finalMsg := ""
	if aborted.Load() {
		if m, ok := abortMsg.Load().(string); ok {
			finalMsg = m
		}
		_ = db.SetAdminRechargeBatchStatus(batchID, batchStatusPaused, finalMsg)
	} else {
		_ = db.SetAdminRechargeBatchStatus(batchID, batchStatusDone, finalMsg)
	}

	db.WriteAudit(operator, "admin_batch_recharge_done",
		fmt.Sprintf("batch=%s plan=%s total=%v success=%v failed=%v unknown=%v",
			batchID, plan, st["total"], st["success"], st["failed"], st["unknown"]), "")
	log.Printf("[batch-recharge] batch=%s done total=%v success=%v failed=%v unknown=%v",
		batchID, st["total"], st["success"], st["failed"], st["unknown"])
}

// mustListItems 查明细，失败时返回空切片（调用点都是尽力而为的收尾逻辑）。
func mustListItems(batchID string) []db.AdminRechargeItem {
	items, err := db.ListAdminRechargeItems(batchID)
	if err != nil {
		log.Printf("[batch-recharge] list items %s failed: %v", batchID, err)
		return nil
	}
	return items
}

func patchItem(crid, status, message string) {
	if err := db.PatchAdminRechargeItem(crid, db.AdminRechargeItemPatch{
		Status: strPtr(status), Message: strPtr(message),
	}); err != nil {
		log.Printf("[batch-recharge] patch item %s failed: %v", crid, err)
	}
}

// endJob 落一个终态并标记 job 已完结。
//
// terminal 必须在这里置位：redemption_token 一旦拿到就一直挂在 job 上，
// 若提交阶段在 preflight/redeem 之前就失败却不标记终态，阶段二的轮询会把
// 这条已判失败的明细重新按上游 result 覆写回 success。
func endJob(job *batchRechargeJob, status, message string) (bool, string) {
	job.terminal = true
	patchItem(job.clientRequestID, status, message)
	return false, ""
}


// submitOneRecharge 走完一条明细的提交阶段。
// 返回 fatal=true 表示遇到需要中止整批的错误（余额不足）。
func submitOneRecharge(cli *cardplatform.Client, plan string, job *batchRechargeJob) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), batchRechargeItemTimeout)
	defer cancel()

	// 1) 发码：count=1，单次超时最多丢一张码的服务费
	patchItem(job.clientRequestID, itemStatusIssuing, "购买内部凭证…")
	issued, err := cli.IssueCDKs(ctx, plan, 1, "brc-issue-"+job.clientRequestID)
	if err != nil {
		if errorCodeOf(err) == "INSUFFICIENT_BALANCE" {
			job.terminal = true
			patchItem(job.clientRequestID, itemStatusFailed, "卡台余额不足，批次已中止")
			return true, "卡台余额不足，批次已中止；请充值后重新提交剩余条目"
		}
		return endJob(job, itemStatusFailed, "购买内部凭证失败："+errorTextOf(err))
	}
	if issued == nil || len(issued.Issued) == 0 || strings.TrimSpace(issued.Issued[0].Code) == "" {
		return endJob(job, itemStatusFailed, "卡台未返回可用凭证")
	}
	one := issued.Issued[0]
	code := strings.TrimSpace(one.Code)
	prefix := strings.TrimSpace(one.CodePrefix)
	if prefix == "" && len(code) >= 14 {
		prefix = code[:14]
	}
	// 落本站码库，保证即使本条失败，这张码也能在 CDK 页面被找回复用
	if err := db.SaveCardplatformCDKCode(one.ID, code, prefix, one.Plan, one.FeeAmountMinor); err != nil {
		log.Printf("[batch-recharge] save cdk failed seq=%d: %v", job.seq, err)
	}
	_ = db.PatchAdminRechargeItem(job.clientRequestID, db.AdminRechargeItemPatch{
		CDKCode: strPtr(code), Status: strPtr(itemStatusPreparing), Message: strPtr("校验凭证…"),
	})

	// 2) preview → redemption_token
	_, previewRaw, err := cli.Preview(ctx, code, "admin-batch-recharge")
	if err != nil {
		return endJob(job, itemStatusFailed, "凭证校验失败："+errorTextOf(err))
	}
	pm := jsonMap(previewRaw)
	token := pickNested(pm, "redemption_token", "token")
	if token == "" {
		return endJob(job, itemStatusFailed, "上游未返回 redemption_token："+upstreamMessage(pm))
	}
	job.redemptionToken = token
	_ = db.PatchAdminRechargeItem(job.clientRequestID, db.AdminRechargeItemPatch{
		RedemptionToken: strPtr(token), Message: strPtr("账号预检…"),
	})

	// 3) preflight → preflight_token（凭据只在这里进内存，不落库）
	credBody := map[string]any{"mode": job.cred.Mode}
	if job.cred.Mode == "mailbox" {
		credBody["email"] = job.cred.Email
		credBody["password"] = job.cred.Password
	} else {
		credBody["session"] = job.cred.Session
	}
	pfStatus, pfRaw, err := cli.Preflight(ctx, map[string]any{
		"code":             code,
		"redemption_token": token,
		"credential":       credBody,
	}, "admin-batch-recharge")
	if err != nil {
		return endJob(job, itemStatusFailed, "账号预检失败："+errorTextOf(err))
	}
	fm := jsonMap(pfRaw)
	if pfStatus < 200 || pfStatus >= 300 {
		msg := upstreamMessage(fm)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", pfStatus)
		}
		// 目标账号已是该套餐：不重试、不扣款
		if strings.Contains(strings.ToUpper(msg), "ALREADY_ACTIVE") {
			return endJob(job, itemStatusSkipped, "账号已是目标套餐，已跳过")
		}
		return endJob(job, itemStatusFailed, "账号预检失败："+msg)
	}
	preflightToken := pickNested(fm, "preflight_token")
	if preflightToken == "" {
		return endJob(job, itemStatusFailed, "上游未返回 preflight_token："+upstreamMessage(fm))
	}
	if email := pickNested(fm, "email", "account_email"); email != "" {
		_ = db.PatchAdminRechargeItem(job.clientRequestID, db.AdminRechargeItemPatch{
			AccountEmail: strPtr(email),
		})
	}

	// 4) redeem —— 结果不确定时绝不重试（docs/danew-cdk-zh.md:72）
	patchItem(job.clientRequestID, itemStatusSubmitted, "已提交，等待开通…")
	rdStatus, rdRaw, err := cli.Redeem(ctx, map[string]any{
		"redemption_token":  token,
		"preflight_token":   preflightToken,
		"client_request_id": job.clientRequestID,
	}, "admin-batch-recharge")
	if err != nil {
		// 传输层失败 = 结果未知：可能上游已受理并扣款，只能轮询确认
		patchItem(job.clientRequestID, itemStatusUnknown, "提交结果未知（网络异常），正在向上游确认，请勿重复提交")
		return false, ""
	}
	rm := jsonMap(rdRaw)
	orderStatus := pickNested(rm, "status")
	message := upstreamMessage(rm)
	if orderID := pickNested(rm, "order_id", "id"); orderID != "" {
		_ = db.PatchAdminRechargeItem(job.clientRequestID, db.AdminRechargeItemPatch{
			UpstreamOrderID: strPtr(orderID),
		})
	}
	if email := pickNested(rm, "account_email", "email"); email != "" {
		_ = db.PatchAdminRechargeItem(job.clientRequestID, db.AdminRechargeItemPatch{
			AccountEmail: strPtr(email),
		})
	}

	// 202 表示已受理进队列，属于正常路径（docs/danew-openapi-zh.md:887）
	if (rdStatus < 200 || rdStatus >= 300) && rdStatus != http.StatusAccepted {
		if message == "" {
			message = fmt.Sprintf("HTTP %d", rdStatus)
		}
		return endJob(job, itemStatusFailed, "提交被拒绝："+message)
	}

	switch {
	case isSuccessOrderStatus(orderStatus):
		return endJob(job, itemStatusSuccess, nonEmptyMsg(message, "开通完成"))
	case isTerminalOrderStatus(orderStatus):
		return endJob(job, itemStatusFailed, nonEmptyMsg(message, "开通失败"))
	}
	// 非终态：留给阶段二轮询，terminal 保持 false
	patchItem(job.clientRequestID, itemStatusRunning, nonEmptyMsg(message, "开通中…"))
	return false, ""
}

// pollBatchRecharge 阶段二：统一轮询未终态明细，直到全部终态或超时。
func pollBatchRecharge(cli *cardplatform.Client, jobs []batchRechargeJob) {
	pending := make([]*batchRechargeJob, 0, len(jobs))
	for i := range jobs {
		j := &jobs[i]
		if !j.terminal && j.redemptionToken != "" {
			pending = append(pending, j)
		}
	}
	if len(pending) == 0 {
		return
	}
	deadline := time.Now().Add(batchRechargePollTimeout)
	for len(pending) > 0 && time.Now().Before(deadline) {
		time.Sleep(batchRechargePollInterval)
		next := pending[:0]
		for _, j := range pending {
			if resolveOneRecharge(cli, j) {
				continue // 已终态
			}
			next = append(next, j)
		}
		pending = next
	}
	for _, j := range pending {
		patchItem(j.clientRequestID, itemStatusUnknown,
			"上游长时间未返回终态，请到「兑换对账」按订单号核对，切勿重复提交")
	}
}

// resolveOneRecharge 查一次上游结果；返回 true 表示已进入终态。
func resolveOneRecharge(cli *cardplatform.Client, j *batchRechargeJob) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, raw, err := cli.Result(ctx, j.redemptionToken, "admin-batch-recharge")
	if err != nil {
		return false // 瞬时错误，下一轮再试
	}
	m := jsonMap(raw)
	st := pickNested(m, "status")
	msg := upstreamMessage(m)
	if email := pickNested(m, "account_email", "email"); email != "" {
		_ = db.PatchAdminRechargeItem(j.clientRequestID, db.AdminRechargeItemPatch{
			AccountEmail: strPtr(email),
		})
	}
	if orderID := pickNested(m, "order_id", "id"); orderID != "" {
		_ = db.PatchAdminRechargeItem(j.clientRequestID, db.AdminRechargeItemPatch{
			UpstreamOrderID: strPtr(orderID),
		})
	}
	switch {
	case isSuccessOrderStatus(st):
		j.terminal = true
		patchItem(j.clientRequestID, itemStatusSuccess, nonEmptyMsg(msg, "开通完成"))
		return true
	case isTerminalOrderStatus(st):
		j.terminal = true
		patchItem(j.clientRequestID, itemStatusFailed, nonEmptyMsg(msg, "开通失败"))
		return true
	}
	patchItem(j.clientRequestID, itemStatusRunning, nonEmptyMsg(msg, "开通中…"))
	return false
}

// ResumeInFlightBatchRecharges 进程重启后把仍在途的明细与上游状态对齐一次。
// 凭据已随进程退出丢失，所以只能查结果，不能重新提交。
func ResumeInFlightBatchRecharges(ctx context.Context) {
	items, err := db.ListInFlightAdminRechargeItems()
	if err != nil {
		log.Printf("[batch-recharge] resume query failed: %v", err)
		return
	}
	if len(items) == 0 {
		return
	}
	log.Printf("[batch-recharge] resume: %d in-flight item(s) to reconcile", len(items))
	go func() {
		cli := cardplatform.NewFromSettings()
		touched := map[string]bool{}
		for _, it := range items {
			select {
			case <-ctx.Done():
				return
			default:
			}
			j := batchRechargeJob{
				seq:             it.Seq,
				clientRequestID: it.ClientRequestID,
				redemptionToken: it.RedemptionToken,
			}
			resolveOneRecharge(cli, &j)
			touched[it.BatchID] = true
		}
		// 重启后不再有 executor 收尾，这里把批次状态落定
		for batchID := range touched {
			rows, err := db.ListAdminRechargeItems(batchID)
			if err != nil {
				continue
			}
			allDone := true
			for _, r := range rows {
				switch r.Status {
				case itemStatusSuccess, itemStatusFailed, itemStatusSkipped, itemStatusUnknown:
				default:
					allDone = false
				}
			}
			if allDone {
				_ = db.SetAdminRechargeBatchStatus(batchID, batchStatusDone, "进程重启后已与上游对齐")
			}
		}
	}()
}

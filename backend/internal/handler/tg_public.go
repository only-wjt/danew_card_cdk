package handler

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/tgmember"
)

var (
	tgPubMu   sync.Mutex
	tgPubHits = map[string][]time.Time{}
)

func tgPublicAllowed(ip string) bool {
	tgPubMu.Lock()
	defer tgPubMu.Unlock()
	now := time.Now()
	cut := now.Add(-time.Minute)
	hits := tgPubHits[ip][:0]
	for _, t := range tgPubHits[ip] {
		if t.After(cut) {
			hits = append(hits, t)
		}
	}
	if len(hits) >= 30 {
		tgPubHits[ip] = hits
		return false
	}
	tgPubHits[ip] = append(hits, now)
	return true
}

func PublicTGPreview(c *gin.Context) {
	if !tgPublicAllowed(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "查询太频繁，请稍后再试"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入卡密"})
		return
	}
	st, err := tgmember.Preview(c.Request.Context(), req.Code)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡密不存在"})
		return
	}
	c.JSON(http.StatusOK, st)
}

func PublicTGQuote(c *gin.Context) {
	if !tgPublicAllowed(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "查询太频繁，请稍后再试"})
		return
	}
	var req struct {
		Code      string `json:"code"`
		Recipient string `json:"recipient"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入卡密和 Telegram 用户名"})
		return
	}
	st, err := tgmember.Quote(c.Request.Context(), req.Code, req.Recipient)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "reusable": true})
		return
	}
	c.JSON(http.StatusOK, st)
}

func PublicTGConfirm(c *gin.Context) {
	if !tgPublicAllowed(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "查询太频繁，请稍后再试"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入卡密"})
		return
	}
	st, err := tgmember.Confirm(c.Request.Context(), req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

func PublicTGResult(c *gin.Context) {
	if !tgPublicAllowed(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "查询太频繁，请稍后再试"})
		return
	}
	st, err := tgmember.Result(strings.TrimSpace(c.Query("code")))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡密不存在"})
		return
	}
	c.JSON(http.StatusOK, st)
}

func AdminTGOverview(c *gin.Context) {
	out, err := db.TGOverview()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tgmember.Ready(); err != nil {
		out["blocked"] = err.Error()
	}
	c.JSON(http.StatusOK, out)
}

func AdminTGIssue(c *gin.Context) {
	var req struct {
		Plan     string `json:"plan"`
		Quantity int    `json:"quantity"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	codes, err := tgmember.Issue(c.Request.Context(), req.Plan, req.Quantity, strings.TrimSpace(req.Note), adminName(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "codes": codes})
		return
	}
	auditAdmin(c, "tg_issue", req.Plan+" x"+strconv.Itoa(len(codes)))
	origin := strings.TrimRight(c.Request.Header.Get("Origin"), "/")
	if origin == "" {
		origin = "https://" + c.Request.Host
	}
	links := make([]string, len(codes))
	for i, code := range codes {
		links[i] = origin + "/recharge?product=tg&code=" + code
	}
	c.JSON(http.StatusOK, gin.H{"codes": codes, "links": links})
}

func AdminTGBatches(c *gin.Context) {
	rows, err := db.ListTGBatches(30)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"batches": rows})
}

func AdminTGRetryBatch(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	codes, err := tgmember.RetryIssue(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "codes": codes})
		return
	}
	c.JSON(http.StatusOK, gin.H{"codes": codes})
}

func AdminTGRecords(c *gin.Context) {
	rows, total, err := db.ListTGRecords(c.Query("group"), c.Query("q"), c.Query("plan"), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	for _, row := range rows {
		if plan, ok := row["plan"].(string); ok {
			row["plan_label"] = tgmember.PlanLabel(plan)
		}
	}
	c.JSON(http.StatusOK, gin.H{"records": rows, "total": total, "limit": 100})
}

func AdminTGRequery(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := tgmember.Requery(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "tg_requery", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func AdminTGResolve(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Note    string `json:"note"`
		Outcome string `json:"outcome"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Note) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写处理备注"})
		return
	}
	if err := tgmember.Resolve(c.Request.Context(), id, strings.TrimSpace(req.Outcome), strings.TrimSpace(req.Note)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "tg_resolve", c.Param("id")+" "+req.Outcome)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func AdminTGDisable(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := tgmember.DisableCode(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "tg_disable", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func AdminTGLimits(c *gin.Context) {
	rows, err := db.ListTGPlanLimits()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"limits": rows, "plans": db.TGPlanKeys()})
}

func AdminSaveTGLimits(c *gin.Context) {
	var req struct {
		Limits []db.TGPlanLimit `json:"limits"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := db.SaveTGPlanLimits(req.Limits); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "tg_limits", strconv.Itoa(len(req.Limits)))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

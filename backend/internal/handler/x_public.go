package handler

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/xlsxsheet"
	"github.com/danew/cdk-recharge-system/internal/xmember"
)

var (
	xPubMu   sync.Mutex
	xPubHits = map[string][]time.Time{}
)

func xPublicAllowed(ip string) bool {
	xPubMu.Lock()
	defer xPubMu.Unlock()
	now := time.Now()
	cut := now.Add(-time.Minute)
	hits := xPubHits[ip][:0]
	for _, t := range xPubHits[ip] {
		if t.After(cut) {
			hits = append(hits, t)
		}
	}
	if len(hits) >= 30 {
		xPubHits[ip] = hits
		return false
	}
	xPubHits[ip] = append(hits, now)
	return true
}

func PublicXPreview(c *gin.Context) {
	if !xPublicAllowed(c.ClientIP()) {
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
	st, err := xmember.Preview(c.Request.Context(), req.Code)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡密不存在"})
		return
	}
	c.JSON(http.StatusOK, st)
}

func PublicXQuote(c *gin.Context) {
	if !xPublicAllowed(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "查询太频繁，请稍后再试"})
		return
	}
	var req struct {
		Code      string `json:"code"`
		Recipient string `json:"recipient"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入卡密和 X 用户名"})
		return
	}
	st, err := xmember.Quote(c.Request.Context(), req.Code, req.Recipient)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "reusable": true})
		return
	}
	c.JSON(http.StatusOK, st)
}

func PublicXConfirm(c *gin.Context) {
	if !xPublicAllowed(c.ClientIP()) {
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
	st, err := xmember.Confirm(c.Request.Context(), req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

func PublicXResult(c *gin.Context) {
	if !xPublicAllowed(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "查询太频繁，请稍后再试"})
		return
	}
	code := strings.TrimSpace(c.Query("code"))
	st, err := xmember.Result(code)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡密不存在"})
		return
	}
	c.JSON(http.StatusOK, st)
}

func AdminXIssue(c *gin.Context) {
	var req struct {
		Plan     string `json:"plan"`
		Channel  string `json:"channel"`
		Quantity int    `json:"quantity"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	by := ""
	if u, ok := c.Get("username"); ok {
		by, _ = u.(string)
	}
	codes, err := xmember.Issue(c.Request.Context(), req.Plan, req.Channel, req.Quantity, strings.TrimSpace(req.Note), by)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "codes": codes})
		return
	}
	auditAdmin(c, "x_issue", req.Channel+" "+req.Plan+" x"+strconv.Itoa(len(codes)))
	links := make([]string, len(codes))
	origin := strings.TrimRight(c.Request.Header.Get("Origin"), "/")
	if origin == "" {
		origin = "https://" + c.Request.Host
	}
	for i, code := range codes {
		links[i] = origin + "/x?code=" + code
	}
	c.JSON(http.StatusOK, gin.H{"codes": codes, "links": links})
}

func AdminXBatches(c *gin.Context) {
	rows, err := db.ListXBatches(30)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"batches": rows})
}

func AdminXRecords(c *gin.Context) {
	rows, total, err := db.ListXRecords(c.Query("group"), c.Query("q"), c.Query("plan"), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"records": rows, "total": total, "limit": 100})
}

func AdminXRequery(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := xmember.Requery(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "x_requery", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func AdminXResolve(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Note    string `json:"note"`
		Outcome string `json:"outcome"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Note) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写处理备注"})
		return
	}
	if err := xmember.Resolve(c.Request.Context(), id, strings.TrimSpace(req.Outcome), strings.TrimSpace(req.Note)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "x_resolve", c.Param("id")+" "+req.Outcome+" "+strings.TrimSpace(req.Note))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func AdminXDisableCode(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := xmember.DisableCode(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "x_disable_code", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func AdminXOverview(c *gin.Context) {
	sum, err := db.XOverview()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sum["strips"] = xmember.ChannelStrips(c.Request.Context())
	c.JSON(http.StatusOK, sum)
}

func AdminXRetryBatch(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	codes, err := xmember.RetryIssue(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "codes": codes})
		return
	}
	auditAdmin(c, "x_retry_batch", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"codes": codes})
}

func AdminXTestQuote(c *gin.Context) {
	var req struct {
		Channel   string `json:"channel"`
		Plan      string `json:"plan"`
		Recipient string `json:"recipient"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写套餐和测试账号"})
		return
	}
	sample, err := xmember.TestQuote(c.Request.Context(), req.Channel, req.Plan, req.Recipient)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "x_test_quote", req.Channel+" "+req.Plan)
	c.JSON(http.StatusOK, gin.H{"sample": sample})
}

func AdminXBatchExport(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	rows, err := db.ListXCodesByBatch(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	origin := "https://" + c.Request.Host
	lines := make([][]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, []string{row.Code, origin + "/x?code=" + row.Code, row.Plan, row.Status})
	}
	c.Header("Content-Disposition", "attachment; filename=x-batch-"+c.Param("id")+".xlsx")
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err := xlsxsheet.Write(c.Writer, "X", []string{"卡密", "兑换链接", "套餐", "状态"}, lines); err != nil {
		c.Status(http.StatusInternalServerError)
	}
}

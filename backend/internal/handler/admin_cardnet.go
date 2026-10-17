package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// 管理端「卡网对接」：客户端、发码请求、记账统计。

type adminCardnetClientBody struct {
	Name             string   `json:"name"`
	AllowedPlans     []string `json:"allowed_plans"`
	AllowedCountries []string `json:"allowed_countries"`
	DailyCodeLimit   *int     `json:"daily_code_limit"`
	IPAllowlist      []string `json:"ip_allowlist"`
	Status           string   `json:"status"`
}

func (b adminCardnetClientBody) input(fallbackLimit int) db.CardnetClientInput {
	limit := fallbackLimit
	if b.DailyCodeLimit != nil {
		limit = *b.DailyCodeLimit
	}
	return db.CardnetClientInput{
		Name: b.Name, AllowedPlans: b.AllowedPlans, AllowedCountries: b.AllowedCountries,
		DailyCodeLimit: limit, IPAllowlist: b.IPAllowlist,
	}
}

func adminCardnetID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

// AdminListCardnetClients GET /api/v1/admin/cardnet/clients
func AdminListCardnetClients(c *gin.Context) {
	list, err := db.ListCardnetClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, cl := range list {
		used, _ := db.CardnetIssuedToday(cl.ID)
		out = append(out, gin.H{"client": cl, "issued_today": used})
	}
	c.JSON(http.StatusOK, gin.H{"list": out})
}

// AdminCreateCardnetClient POST /api/v1/admin/cardnet/clients — Key 只在这里返回一次。
func AdminCreateCardnetClient(c *gin.Context) {
	var body adminCardnetClientBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	client, key, err := db.CreateCardnetClient(body.input(200))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "cardnet_client_create", fmt.Sprintf("id=%d name=%s plans=%s", client.ID, client.Name,
		strings.Join(client.AllowedPlans, ",")))
	c.JSON(http.StatusOK, gin.H{"client": client, "api_key": key})
}

// AdminUpdateCardnetClient PUT /api/v1/admin/cardnet/clients/:id
func AdminUpdateCardnetClient(c *gin.Context) {
	id, ok := adminCardnetID(c)
	if !ok {
		return
	}
	current, err := db.GetCardnetClient(id)
	if err != nil {
		writeCardnetAdminErr(c, err)
		return
	}
	var body adminCardnetClientBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	client, err := db.UpdateCardnetClient(id, body.input(current.DailyCodeLimit))
	if err != nil {
		writeCardnetAdminErr(c, err)
		return
	}
	if s := strings.TrimSpace(body.Status); s != "" && s != client.Status {
		if err := db.SetCardnetClientStatus(id, s); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		client, _ = db.GetCardnetClient(id)
	}
	auditAdmin(c, "cardnet_client_update", fmt.Sprintf("id=%d status=%s limit=%d plans=%s countries=%s ips=%d",
		id, client.Status, client.DailyCodeLimit, strings.Join(client.AllowedPlans, ","),
		strings.Join(client.AllowedCountries, ","), len(client.IPAllowlist)))
	c.JSON(http.StatusOK, gin.H{"client": client})
}

// AdminRotateCardnetClientKey POST /api/v1/admin/cardnet/clients/:id/rotate-key
func AdminRotateCardnetClientKey(c *gin.Context) {
	id, ok := adminCardnetID(c)
	if !ok {
		return
	}
	key, err := db.RotateCardnetClientKey(id)
	if err != nil {
		writeCardnetAdminErr(c, err)
		return
	}
	auditAdmin(c, "cardnet_client_rotate_key", fmt.Sprintf("id=%d", id))
	client, _ := db.GetCardnetClient(id)
	c.JSON(http.StatusOK, gin.H{"client": client, "api_key": key})
}

// AdminListCardnetRequests GET /api/v1/admin/cardnet/requests
func AdminListCardnetRequests(c *gin.Context) {
	clientID, _ := strconv.ParseInt(c.Query("client_id"), 10, 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := db.ListCardnetRequests(db.CardnetRequestQuery{
		ClientID: clientID, Status: c.Query("status"), Query: c.Query("q"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": total, "page": page, "page_size": pageSize})
}

// AdminGetCardnetRequest GET /api/v1/admin/cardnet/requests/:id — 含发出的码。
func AdminGetCardnetRequest(c *gin.Context) {
	id, ok := adminCardnetID(c)
	if !ok {
		return
	}
	req, err := db.GetCardnetRequest(id)
	if err != nil {
		writeCardnetAdminErr(c, err)
		return
	}
	codes, err := db.ListCardnetRequestCodes(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req, "codes": codes})
}

// AdminRefetchCardnetRequest POST /api/v1/admin/cardnet/requests/:id/refetch
// 用同一个上游幂等键再调一次卡台：卡台重放原批次，张数对上就转成功。
func AdminRefetchCardnetRequest(c *gin.Context) {
	id, ok := adminCardnetID(c)
	if !ok {
		return
	}
	req, err := db.GetCardnetRequest(id)
	if err != nil {
		writeCardnetAdminErr(c, err)
		return
	}
	if req.Status != db.CardnetRequestPending && req.Status != db.CardnetRequestReview {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只有待处理或待核对的请求可以重新拉取"})
		return
	}
	status, out := runCardnetIssue(context.WithoutCancel(c.Request.Context()), req, true)
	auditAdmin(c, "cardnet_request_refetch", fmt.Sprintf("request=%s http=%d", req.RequestID, status))
	fresh, _ := db.GetCardnetRequest(id)
	c.JSON(http.StatusOK, gin.H{"http_status": status, "result": out, "request": fresh})
}

// AdminFailCardnetRequest POST /api/v1/admin/cardnet/requests/:id/mark-failed
// 人工确认卡台没扣费（或已在卡台手动作废）后才可用：卡网随后会换新键重发。
func AdminFailCardnetRequest(c *gin.Context) {
	id, ok := adminCardnetID(c)
	if !ok {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&body)
	note := strings.TrimSpace(body.Note)
	if note == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写核对说明"})
		return
	}
	req, err := db.GetCardnetRequest(id)
	if err != nil {
		writeCardnetAdminErr(c, err)
		return
	}
	done, err := db.FailCardnetRequest(id, "MANUAL_FAILED", "人工标记失败："+note)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !done {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只有待处理或待核对的请求可以标记失败"})
		return
	}
	auditAdmin(c, "cardnet_request_mark_failed", fmt.Sprintf("request=%s note=%s", req.RequestID, note))
	fresh, _ := db.GetCardnetRequest(id)
	c.JSON(http.StatusOK, gin.H{"request": fresh})
}

// AdminCardnetStats GET /api/v1/admin/cardnet/stats?client_id=&from=YYYY-MM-DD&to=YYYY-MM-DD
func AdminCardnetStats(c *gin.Context) {
	clientID, _ := strconv.ParseInt(c.Query("client_id"), 10, 64)
	rows, err := db.CardnetStats(clientID, c.Query("from"), c.Query("to"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var codes, consumed, disabled int
	var fee int64
	for _, r := range rows {
		codes += r.Codes
		consumed += r.Consumed
		disabled += r.Disabled
		fee += r.FeeMinor
	}
	c.JSON(http.StatusOK, gin.H{
		"rows": rows,
		"total": gin.H{
			"codes": codes, "consumed": consumed, "disabled": disabled, "fee_minor": fee,
		},
	})
}

func writeCardnetAdminErr(c *gin.Context, err error) {
	if errors.Is(err, db.ErrCardnetNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "记录不存在"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

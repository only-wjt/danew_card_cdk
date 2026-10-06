package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/xmember"
)

// X 供货：每个 X 套餐只从一家卡台出码（SpaceX 或 Avanfinity），在这里二选一。
// 设置存在 site_settings 的 x_supply_<key>，值为 spacex / avan / off。
// 新文件，避免和 upstream 的 x_public.go / cardplatform_cdk.go 冲突。

type xSupplyPlan struct {
	Key      string
	Label    string
	SpaceX   string // SpaceX 卡台侧 plan key（带 x_ 前缀），空 = SpaceX 不卖
	Avan     string // Avanfinity 侧 plan key，空 = Avan 不卖
	Fallback string // 未设置时的默认来源
}

var xSupplyPlans = []xSupplyPlan{
	{Key: "basic_monthly", Label: "Basic · 月付", SpaceX: "x_basic_monthly", Fallback: "spacex"},
	{Key: "basic_yearly", Label: "Basic · 年付", SpaceX: "x_basic_yearly", Fallback: "spacex"},
	{Key: "premium_monthly", Label: "Premium · 月付", SpaceX: "x_premium_monthly", Fallback: "spacex"},
	{Key: "premium_3m", Label: "Premium · 3 个月", Avan: "premium_3m", Fallback: "avan"},
	{Key: "premium_6m", Label: "Premium · 6 个月", Avan: "premium_6m", Fallback: "avan"},
	{Key: "premium_yearly", Label: "Premium · 12 个月", SpaceX: "x_premium_yearly", Avan: "premium_12m", Fallback: "avan"},
	{Key: "premium_plus_monthly", Label: "Premium+ · 月付", SpaceX: "x_premium_plus_monthly", Fallback: "spacex"},
	{Key: "premium_plus_3m", Label: "Premium+ · 3 个月", Avan: "premium_plus_3m", Fallback: "avan"},
	{Key: "premium_plus_6m", Label: "Premium+ · 6 个月", Avan: "premium_plus_6m", Fallback: "avan"},
	{Key: "premium_plus_yearly", Label: "Premium+ · 12 个月", SpaceX: "x_premium_plus_yearly", Avan: "premium_plus_12m", Fallback: "avan"},
}

func xSupplyFind(key string) (xSupplyPlan, bool) {
	for _, p := range xSupplyPlans {
		if p.Key == key {
			return p, true
		}
	}
	return xSupplyPlan{}, false
}

func (p xSupplyPlan) options() []string {
	out := []string{}
	if p.SpaceX != "" {
		out = append(out, "spacex")
	}
	if p.Avan != "" {
		out = append(out, "avan")
	}
	return out
}

func (p xSupplyPlan) supports(source string) bool {
	return (source == "spacex" && p.SpaceX != "") || (source == "avan" && p.Avan != "")
}

// xSupplySource 返回当前来源；off 表示停售。
func xSupplySource(p xSupplyPlan) string {
	v, _ := db.GetSetting("x_supply_" + p.Key)
	v = strings.TrimSpace(v)
	if v == "off" || p.supports(v) {
		return v
	}
	return p.Fallback
}

func xSupplyRows() []gin.H {
	rows := make([]gin.H, 0, len(xSupplyPlans))
	for _, p := range xSupplyPlans {
		src := xSupplySource(p)
		rows = append(rows, gin.H{
			"key":         p.Key,
			"label":       p.Label,
			"source":      src,
			"options":     p.options(),
			"enabled":     src != "off",
			"spacex_plan": p.SpaceX,
			"avan_plan":   p.Avan,
		})
	}
	return rows
}

// AdminGetXSupply GET /api/v1/admin/x/supply
func AdminGetXSupply(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"plans": xSupplyRows()})
}

// AdminSaveXSupply PUT /api/v1/admin/x/supply  body: {key, source}
func AdminSaveXSupply(c *gin.Context) {
	var req struct {
		Key    string `json:"key"`
		Source string `json:"source"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	p, ok := xSupplyFind(strings.TrimSpace(req.Key))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知套餐"})
		return
	}
	src := strings.TrimSpace(req.Source)
	if src != "off" && !p.supports(src) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "这个套餐不能从该卡台出码"})
		return
	}
	prev := xSupplySource(p)
	if err := db.SetSetting("x_supply_"+p.Key, src); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "x_supply_switch", p.Key+" "+prev+" -> "+src)
	c.JSON(http.StatusOK, gin.H{"plans": xSupplyRows()})
}

// AdminXSupplyIssue POST /api/v1/admin/x/supply/issue
// body: {plan, quantity, note, payment_country}；按当前来源分流，统一返回 {source, codes, links}。
func AdminXSupplyIssue(c *gin.Context) {
	var req struct {
		Plan           string `json:"plan"`
		Quantity       int    `json:"quantity"`
		Note           string `json:"note"`
		PaymentCountry string `json:"payment_country"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	p, ok := xSupplyFind(strings.TrimSpace(req.Plan))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知套餐"})
		return
	}
	if req.Quantity < 1 {
		req.Quantity = 1
	}
	origin := strings.TrimRight(c.Request.Header.Get("Origin"), "/")
	if origin == "" {
		origin = "https://" + c.Request.Host
	}
	switch xSupplySource(p) {
	case "avan":
		by := ""
		if u, ok := c.Get("username"); ok {
			by, _ = u.(string)
		}
		codes, err := xmember.Issue(c.Request.Context(), p.Avan, db.XChannelCDK, req.Quantity, strings.TrimSpace(req.Note), by)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "codes": codes, "source": "avan"})
			return
		}
		auditAdmin(c, "x_issue", "supply=avan "+p.Avan+" x"+strconv.Itoa(len(codes)))
		links := make([]string, len(codes))
		for i, code := range codes {
			links[i] = origin + "/x?code=" + code
		}
		c.JSON(http.StatusOK, gin.H{"source": "avan", "codes": codes, "links": links})
	case "spacex":
		xSupplyIssueSpaceX(c, p, req.Quantity, req.PaymentCountry, origin)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "这个套餐已停售，先在「供货设置」里选卡台"})
	}
}

// xSupplyIssueSpaceX 复用 CardPlatformIssueCDKs：改写请求体后转调，再把响应整理成统一格式。
func xSupplyIssueSpaceX(c *gin.Context, p xSupplyPlan, qty int, country, origin string) {
	body, _ := json.Marshal(gin.H{
		"plan":              p.SpaceX,
		"count":             qty,
		"funding_confirmed": true,
		"payment_country":   country,
	})
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	rec := &bufferedWriter{ResponseWriter: c.Writer, status: http.StatusOK}
	c.Writer = rec
	CardPlatformIssueCDKs(c)
	c.Writer = rec.ResponseWriter

	var out map[string]any
	_ = json.Unmarshal(rec.buf.Bytes(), &out)
	if rec.status >= 400 || out == nil {
		if out == nil {
			out = gin.H{"error": "卡台发码失败"}
		}
		out["source"] = "spacex"
		c.JSON(rec.status, out)
		return
	}
	codes := []string{}
	if items, ok := out["issued"].([]any); ok {
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				if code, _ := m["code"].(string); code != "" {
					codes = append(codes, code)
				}
			}
		}
	}
	links := make([]string, len(codes))
	for i, code := range codes {
		links[i] = origin + "/recharge?cdk=" + code
	}
	resp := gin.H{"source": "spacex", "codes": codes, "links": links}
	if pe, ok := out["partial_error"]; ok {
		resp["partial_error"] = pe
	}
	c.JSON(http.StatusOK, resp)
}

// bufferedWriter 截住内层 handler 的输出，由外层统一回写。
type bufferedWriter struct {
	gin.ResponseWriter
	buf    bytes.Buffer
	status int
}

func (w *bufferedWriter) WriteHeader(code int) { w.status = code }
func (w *bufferedWriter) WriteHeaderNow()      {}
func (w *bufferedWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}
func (w *bufferedWriter) WriteString(s string) (int, error) {
	return w.buf.WriteString(s)
}
func (w *bufferedWriter) Status() int   { return w.status }
func (w *bufferedWriter) Written() bool { return w.buf.Len() > 0 }
func (w *bufferedWriter) Size() int     { return w.buf.Len() }

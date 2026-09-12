package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/gin-gonic/gin"
)

// 公开批量查询上限：每条可能回源卡台 Result，避免一次打爆上游。
const lookupBatchMax = 100
const lookupBatchWorkers = 4

type cdkLookupResult struct {
	CDKCode      string  `json:"cdk_code"`
	Status       string  `json:"status"` // unused | used | failed | disabled | expired | processing | unknown
	Used         bool    `json:"used"`
	CanResubmit  bool    `json:"can_resubmit"`
	AccountEmail string  `json:"account_email,omitempty"`
	Plan         string  `json:"plan,omitempty"`
	UsedAt       *string `json:"used_at,omitempty"`
	Notes        string  `json:"notes,omitempty"`
	Message      string  `json:"message"`
}

// LookupCDKStatus GET /api/v1/lookup/cdk?code=  或兼容 ?cdk_code=
func LookupCDKStatus(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		code = strings.TrimSpace(c.Query("cdk_code"))
	}
	code = strings.TrimSpace(code)
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入卡密"})
		return
	}

	resp := lookupOneCDK(c.Request.Context(), code, deviceFrom(c))
	if resp.Status == "unknown" {
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "未找到该卡密记录",
			"cdk_code": code,
			"status":   "unknown",
			"used":     false,
			"message":  "未找到该卡密。请确认输入完整卡密。",
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// LookupCDKStatusBatch POST /api/v1/lookup/cdk/batch
// body: { "codes": ["SXC-…"], "text": "可选整段粘贴" }
func LookupCDKStatusBatch(c *gin.Context) {
	var req struct {
		Codes []string `json:"codes"`
		Text  string   `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	codes := normalizeLookupCodes(append(append([]string{}, req.Codes...), splitLookupText(req.Text)...))
	if len(codes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入至少一张卡密"})
		return
	}
	if len(codes) > lookupBatchMax {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "一次最多查询 " + strconv.Itoa(lookupBatchMax) + " 张卡密",
			"max":   lookupBatchMax,
		})
		return
	}

	results := make([]cdkLookupResult, len(codes))
	sem := make(chan struct{}, lookupBatchWorkers)
	var wg sync.WaitGroup
	ctx := c.Request.Context()
	device := deviceFrom(c)
	for i, code := range codes {
		wg.Add(1)
		go func(i int, code string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = lookupOneCDK(ctx, code, device)
		}(i, code)
	}
	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"total":   len(results),
		"max":     lookupBatchMax,
		"results": results,
	})
}

func splitLookupText(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.FieldsFunc(raw, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';' || r == '，' || r == '；'
	})
}

func normalizeLookupCodes(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		code := strings.ToUpper(strings.TrimSpace(item))
		if len(code) < 4 {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func applyLookupFailure(resp *cdkLookupResult, notes string, reusable bool) {
	resp.Status = "failed"
	resp.Used = false
	resp.CanResubmit = reusable
	note := strings.TrimSpace(notes)
	if note != "" {
		resp.Notes = note
	}
	if reusable {
		resp.Message = "使用失败，卡密已重新激活，可以重新提交"
	} else {
		resp.Message = "使用失败，卡密尚未重新激活，请等待处理后再查"
	}
	if note != "" {
		resp.Message = resp.Message + "：" + note
	}
}

func lookupOneCDK(ctx context.Context, code, deviceID string) cdkLookupResult {
	resp := cdkLookupResult{CDKCode: code, Status: "unknown", Message: "未找到该卡密记录"}
	if db.DB == nil {
		return resp
	}
	redeemOK := false
	reusable := false

	var planType, keyStatus string
	var usedAt, expiresAt sql.NullTime
	err := db.DB.QueryRow(`
		SELECT COALESCE(plan_type,''), COALESCE(status,''), used_at, expires_at
		FROM cd_keys WHERE upper(trim(code)) = upper(trim(?))
	`, code).Scan(&planType, &keyStatus, &usedAt, &expiresAt)
	if err == nil {
		resp.Plan = planType
		switch strings.ToLower(keyStatus) {
		case "used":
			resp.Status, resp.Used = "used", true
			resp.Message = "卡密使用成功"
			reusable = false
		case "disabled":
			resp.Status, resp.Used = "disabled", false
			resp.Message = "卡密已禁用"
		case "expired":
			resp.Status, resp.Used = "expired", false
			resp.Message = "卡密已过期"
		case "active", "":
			resp.Status, resp.Used = "unused", false
			resp.Message = "卡密未使用"
			reusable = true
		default:
			resp.Status = strings.ToLower(keyStatus)
			resp.Message = "卡密状态：" + keyStatus
		}
		if expiresAt.Valid && expiresAt.Time.Before(time.Now()) && resp.Status == "unused" {
			resp.Status, resp.Message = "expired", "卡密已过期"
			reusable = false
		}
		if usedAt.Valid {
			s := usedAt.Time.Format("2006-01-02 15:04:05")
			resp.UsedAt = &s
		}
	} else if err != sql.ErrNoRows {
		log.Printf("[lookup-cdk] cd_keys: %v", err)
	}

	var cpStatus, cpPlan string
	err = db.DB.QueryRow(`
		SELECT COALESCE(status,''), COALESCE(plan,'')
		FROM cardplatform_cdk_codes WHERE upper(trim(code)) = upper(trim(?))
		ORDER BY created_at DESC LIMIT 1
	`, code).Scan(&cpStatus, &cpPlan)
	if err == nil {
		if resp.Plan == "" {
			resp.Plan = cpPlan
		}
		st := strings.ToLower(strings.TrimSpace(cpStatus))
		if st == "" {
			st = "unused"
		}
		switch st {
		case "used", "redeemed", "consumed":
			resp.Status, resp.Used = "used", true
			resp.Message = "卡密使用成功"
			reusable = false
		case "disabled":
			if resp.Status != "used" {
				resp.Status, resp.Used = "disabled", false
				resp.Message = "卡密已禁用"
			}
			reusable = false
		case "unused", "active":
			if resp.Status == "unknown" {
				resp.Status, resp.Used = "unused", false
				resp.Message = "卡密未使用"
			}
			if resp.Status == "unused" {
				reusable = true
			}
		}
	}

	var taskStatus string
	var accountEmail, taskNotes sql.NullString
	var taskCompleted sql.NullTime
	err = db.DB.QueryRow(`
		SELECT COALESCE(task_status,''), account_email, completed_at, COALESCE(notes,'')
		FROM recharge_tasks
		WHERE upper(trim(cdk_code)) = upper(trim(?))
		ORDER BY created_at DESC LIMIT 1
	`, code).Scan(&taskStatus, &accountEmail, &taskCompleted, &taskNotes)
	if err == nil {
		if accountEmail.Valid && strings.TrimSpace(accountEmail.String) != "" {
			resp.AccountEmail = strings.TrimSpace(accountEmail.String)
		}
		ts := strings.ToLower(taskStatus)
		switch ts {
		case "completed", "success", "done":
			redeemOK = true
			resp.Status, resp.Used = "used", true
			resp.Message = "卡密使用成功"
			reusable = false
			if taskCompleted.Valid {
				s := taskCompleted.Time.Format("2006-01-02 15:04:05")
				resp.UsedAt = &s
			}
		case "pending", "submitted", "running", "queued", "processing":
			if !redeemOK && resp.Status != "used" {
				resp.Status, resp.Used = "processing", false
				resp.Message = "卡密兑换处理中"
			}
		case "failed", "declined", "cancelled":
			if !redeemOK {
				note := ""
				if taskNotes.Valid {
					note = taskNotes.String
				}
				applyLookupFailure(&resp, note, reusable)
			}
		}
	}

	if bind, berr := db.GetBindingByCDK(code); berr == nil && bind != nil {
		if resp.AccountEmail == "" && strings.TrimSpace(bind.SessionPayload) != "" {
			if em := extractEmailFromSession(bind.SessionPayload); em != "" {
				resp.AccountEmail = em
			}
		}
		if tok := strings.TrimSpace(bind.RedemptionToken); tok != "" {
			cli := cardClientOrSettings(db.CardPlatformAccountIDForCode(code))
			st, raw, rerr := cli.Result(ctx, tok, deviceID)
			if rerr == nil && st >= 200 && st < 300 && len(raw) > 0 {
				var payload map[string]any
				if json.Unmarshal(raw, &payload) == nil && payload != nil {
					orderStatus := strAny(payload["status"])
					if orderStatus == "" {
						if order, ok := payload["order"].(map[string]any); ok {
							orderStatus = strAny(order["status"])
						}
					}
					email := strAny(payload["account_email"])
					if email == "" {
						if order, ok := payload["order"].(map[string]any); ok {
							email = strAny(order["account_email"])
						}
					}
					if email != "" {
						resp.AccountEmail = email
					}
					os := strings.ToLower(orderStatus)
					switch {
					case os == "completed" || os == "success" || os == "done" || os == "paid":
						redeemOK = true
						resp.Status, resp.Used = "used", true
						resp.CanResubmit = false
						resp.Message = "卡密使用成功"
					case os == "failed" || os == "declined" || os == "cancelled":
						if !redeemOK {
							applyLookupFailure(&resp, "", reusable)
						}
					case os != "":
						if !redeemOK && resp.Status != "used" && resp.Status != "failed" {
							resp.Status, resp.Used = "processing", false
							resp.Message = "卡密兑换处理中"
						}
					}
				}
			}
		}
	}

	if resp.AccountEmail != "" && resp.Status == "unused" && !resp.CanResubmit {
		resp.Status, resp.Used = "used", true
		resp.Message = "卡密使用成功"
	}
	if resp.Status == "used" {
		resp.Used = true
		resp.CanResubmit = false
		if resp.Message == "" || resp.Message == "未找到该卡密记录" {
			resp.Message = "卡密使用成功"
		}
	}
	if resp.Status == "unknown" {
		resp.Message = "未找到该卡密。请确认输入完整卡密。"
	}
	return resp
}

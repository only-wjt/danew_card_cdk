package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// AdminXConnectAvan POST /api/v1/admin/x/connect-avan
// 用已配好的 avanfinity（2026-08，GPT 备台）凭证开一行 X 会员账户。
// 两种协议走同一套 App ID / Key，只是能力分开记，免得用户再粘一遍凭证。
func AdminXConnectAvan(c *gin.Context) {
	accounts, err := db.ListCardPlatformAccounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var src *db.CardPlatformAccount
	for i := range accounts {
		a := accounts[i]
		if a.Protocol == db.AccountProtocolAvanfinityAPIv1 {
			c.JSON(http.StatusOK, gin.H{"id": a.ID, "created": false})
			return
		}
		if src == nil && a.Protocol == db.AccountProtocolAvanfinity202608 &&
			strings.TrimSpace(a.CredPublic) != "" && strings.TrimSpace(a.CredSecret) != "" {
			src = &accounts[i]
		}
	}
	if src == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有填好凭证的 avanfinity 卡台，先在卡台页配好 App ID 和 Key"})
		return
	}
	id, err := db.UpsertCardPlatformAccount(db.CardPlatformAccount{
		Name:         src.Name + " · X",
		Protocol:     db.AccountProtocolAvanfinityAPIv1,
		Capabilities: db.CapXCDK,
		SiteBase:     src.SiteBase,
		CredPublic:   src.CredPublic,
		CredSecret:   src.CredSecret,
		Status:       "active",
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "x_connect_avan", src.Name)
	c.JSON(http.StatusOK, gin.H{"id": id, "created": true})
}

// AdminDeleteCardPlatform POST /api/v1/admin/card-platforms/delete
// 有在途码、开着的 X 通道或是 GPT 主台时拒绝，提示先停用。
func AdminDeleteCardPlatform(c *gin.Context) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	acc, err := db.GetCardPlatformAccount(req.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡台不存在"})
		return
	}
	if err := db.DeleteCardPlatformAccount(req.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "delete_card_platform", fmt.Sprintf("id=%d name=%s protocol=%s", acc.ID, acc.Name, acc.Protocol))
	AdminListCardPlatforms(c)
}

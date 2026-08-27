package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/gin-gonic/gin"
)

type localStockImportBody struct {
	Plan  string   `json:"plan"`
	Text  string   `json:"text"`
	Codes []string `json:"codes"`
}

// AdminGetLocalStockSummary GET /api/v1/admin/local-stock/summary
func AdminGetLocalStockSummary(c *gin.Context) {
	list, err := db.ListLocalStockSummaries()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list})
}

// AdminImportLocalStock POST /api/v1/admin/local-stock/import
func AdminImportLocalStock(c *gin.Context) {
	var req localStockImportBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	plan := strings.TrimSpace(req.Plan)
	if plan == "" {
		plan = db.PlanGPTWhite
	}
	codes := append([]string{}, req.Codes...)
	if strings.TrimSpace(req.Text) != "" {
		codes = append(codes, db.ParseLocalStockImportLines(req.Text)...)
	}
	imported, skipped, err := db.ImportLocalStockCodes(plan, codes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "local_stock_import",
		"plan="+plan+" imported="+strconv.Itoa(imported)+" skipped="+strconv.Itoa(len(skipped)))
	summary, _ := db.ListLocalStockSummaries()
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"imported": imported,
		"skipped":  skipped,
		"summary":  summary,
	})
}

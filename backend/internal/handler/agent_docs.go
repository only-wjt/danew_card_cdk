package handler

import (
	"net/http"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/apidocs"
	"github.com/gin-gonic/gin"
)

// requestBaseURL 还原当前站点的对外地址，供规范里的 servers 使用。
// 站点通常跑在反代后面，优先信任 X-Forwarded-Proto。
func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = strings.ToLower(strings.Split(proto, ",")[0])
	} else if c.Request.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// AgentOpenAPIJSON GET /api/v1/agent/openapi.json
// servers 改写成当前站点地址，下载后可直接导入 Postman / Apifox 使用。
func AgentOpenAPIJSON(c *gin.Context) {
	spec, err := apidocs.AgentSpecJSON(requestBaseURL(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "API 规范生成失败"})
		return
	}
	if c.Query("download") != "" {
		c.Header("Content-Disposition", `attachment; filename="agent-openapi.json"`)
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", spec)
}

// AgentOpenAPIYAML GET /api/v1/agent/openapi.yaml
func AgentOpenAPIYAML(c *gin.Context) {
	if c.Query("download") != "" {
		c.Header("Content-Disposition", `attachment; filename="agent-openapi.yaml"`)
	}
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", apidocs.AgentSpecYAML())
}

// AgentOpenAPIMarkdown GET /api/v1/agent/openapi.md
// 与 JSON/YAML 同源渲染，接口改了三份产物一起变。
func AgentOpenAPIMarkdown(c *gin.Context) {
	md, err := apidocs.RenderMarkdown()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "文档生成失败"})
		return
	}
	if c.Query("download") != "" {
		c.Header("Content-Disposition", `attachment; filename="agent-api-zh.md"`)
	}
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(md))
}

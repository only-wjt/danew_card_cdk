// Package apidocs 内嵌并分发代理开放 API 的 OpenAPI 规范。
//
// YAML 是唯一来源，JSON 由它转换而来，两份不会漂移。
package apidocs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed agent-openapi.yaml
var agentSpecYAML []byte

var (
	once      sync.Once
	specDoc   map[string]any
	parseErr  error
	jsonCache []byte
)

func load() {
	once.Do(func() {
		if err := yaml.Unmarshal(agentSpecYAML, &specDoc); err != nil {
			parseErr = fmt.Errorf("解析 agent-openapi.yaml 失败: %w", err)
			return
		}
		jsonCache, parseErr = json.Marshal(specDoc)
	})
}

// AgentSpecYAML 返回原始 YAML。
func AgentSpecYAML() []byte { return agentSpecYAML }

// AgentSpecJSON 返回 JSON 形式的规范，servers 会被改写成传入的 baseURL，
// 这样代理下载后导入 Postman/Apifox 可以直接发请求，不用手工改 host。
func AgentSpecJSON(baseURL string) ([]byte, error) {
	load()
	if parseErr != nil {
		return nil, parseErr
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return jsonCache, nil
	}
	// 浅拷贝顶层即可：只改 servers 这一个键，其余子树共享不会被写。
	doc := make(map[string]any, len(specDoc))
	for k, v := range specDoc {
		doc[k] = v
	}
	doc["servers"] = []any{
		map[string]any{"url": strings.TrimSuffix(baseURL, "/") + "/api/v1", "description": "当前站点"},
	}
	return json.Marshal(doc)
}

// jsonIndent 供 markdown 渲染示例块使用。
func jsonIndent(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Version 取规范里声明的版本号，供前端展示。
func Version() string {
	load()
	if parseErr != nil || specDoc == nil {
		return ""
	}
	info, _ := specDoc["info"].(map[string]any)
	if info == nil {
		return ""
	}
	v, _ := info["version"].(string)
	return v
}

package apidocs

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RenderMarkdown 把 OpenAPI 规范渲染成一份人读文档。
//
// 与 JSON/YAML 出自同一份 YAML 源，改接口只需改规范，三份产物一起变，不会漂移。
func RenderMarkdown() (string, error) {
	load()
	if parseErr != nil {
		return "", parseErr
	}
	var b strings.Builder
	info := mapOf(specDoc["info"])

	fmt.Fprintf(&b, "# %s\n\n", str(info["title"]))
	if v := str(info["version"]); v != "" {
		fmt.Fprintf(&b, "> 版本 `%s`　·　本文由 `backend/internal/apidocs/agent-openapi.yaml` 自动生成，请勿手工编辑。\n\n", v)
	}
	if d := str(info["description"]); d != "" {
		b.WriteString(strings.TrimSpace(dedent(d)) + "\n\n")
	}

	paths := mapOf(specDoc["paths"])
	routes := sortedRoutes(paths)

	b.WriteString("## 接口一览\n\n")
	b.WriteString("| 方法 | 路径 | 说明 |\n| --- | --- | --- |\n")
	for _, r := range routes {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", strings.ToUpper(r.method), r.path, str(r.op["summary"]))
	}
	b.WriteString("\n")

	b.WriteString("## 接口详情\n\n")
	for _, r := range routes {
		renderOperation(&b, r)
	}

	renderSchemas(&b)
	return b.String(), nil
}

type route struct {
	path   string
	method string
	op     map[string]any
}

var methodOrder = []string{"get", "post", "put", "patch", "delete"}

func sortedRoutes(paths map[string]any) []route {
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]route, 0, len(keys))
	for _, p := range keys {
		item := mapOf(paths[p])
		for _, m := range methodOrder {
			if op, ok := item[m]; ok {
				out = append(out, route{path: p, method: m, op: mapOf(op)})
			}
		}
	}
	return out
}

func renderOperation(b *strings.Builder, r route) {
	fmt.Fprintf(b, "### `%s %s` %s\n\n", strings.ToUpper(r.method), r.path, str(r.op["summary"]))
	if d := str(r.op["description"]); d != "" {
		b.WriteString(strings.TrimSpace(dedent(d)) + "\n\n")
	}

	if params := sliceOf(r.op["parameters"]); len(params) > 0 {
		b.WriteString("**参数**\n\n")
		b.WriteString("| 名称 | 位置 | 必填 | 类型 | 说明 |\n| --- | --- | --- | --- | --- |\n")
		for _, raw := range params {
			p := mapOf(raw)
			required := "否"
			if v, _ := p["required"].(bool); v {
				required = "是"
			}
			fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n",
				str(p["name"]), str(p["in"]), required,
				schemaTypeName(mapOf(p["schema"])), inlineText(str(p["description"])))
		}
		b.WriteString("\n")
	}

	if rb := mapOf(r.op["requestBody"]); len(rb) > 0 {
		b.WriteString("**请求体**\n\n")
		if ex := firstExample(rb); ex != "" {
			b.WriteString("```json\n" + ex + "\n```\n\n")
		}
		if schema := jsonSchemaOf(rb); len(schema) > 0 {
			renderProperties(b, schema)
		}
	}

	if resps := mapOf(r.op["responses"]); len(resps) > 0 {
		codes := make([]string, 0, len(resps))
		for k := range resps {
			codes = append(codes, k)
		}
		sort.Strings(codes)
		b.WriteString("**响应**\n\n")
		b.WriteString("| 状态码 | 说明 |\n| --- | --- |\n")
		for _, code := range codes {
			fmt.Fprintf(b, "| `%s` | %s |\n", code, inlineText(str(resolveRef(mapOf(resps[code]))["description"])))
		}
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
}

func renderProperties(b *strings.Builder, schema map[string]any) {
	props := mapOf(schema["properties"])
	if len(props) == 0 {
		return
	}
	required := map[string]bool{}
	for _, v := range sliceOf(schema["required"]) {
		required[str(v)] = true
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString("| 字段 | 类型 | 必填 | 说明 |\n| --- | --- | --- | --- |\n")
	for _, k := range keys {
		p := mapOf(props[k])
		req := "否"
		if required[k] {
			req = "是"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n",
			k, schemaTypeName(p), req, inlineText(str(p["description"])))
	}
	b.WriteString("\n")
}

func renderSchemas(b *strings.Builder) {
	comps := mapOf(specDoc["components"])
	schemas := mapOf(comps["schemas"])
	if len(schemas) == 0 {
		return
	}
	// Webhook 说明篇幅最长也最关键，单独提到前面作为一节。
	if wh := mapOf(schemas["WebhookEvent"]); len(wh) > 0 {
		b.WriteString("## Webhook 回调\n\n")
		b.WriteString(strings.TrimSpace(dedent(str(wh["description"]))) + "\n\n")
		renderProperties(b, wh)
		b.WriteString("---\n\n")
	}

	keys := make([]string, 0, len(schemas))
	for k := range schemas {
		if k == "WebhookEvent" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString("## 数据结构\n\n")
	for _, k := range keys {
		s := mapOf(schemas[k])
		fmt.Fprintf(b, "### %s\n\n", k)
		if d := str(s["description"]); d != "" {
			b.WriteString(strings.TrimSpace(dedent(d)) + "\n\n")
		}
		if enum := sliceOf(s["enum"]); len(enum) > 0 {
			vals := make([]string, 0, len(enum))
			for _, v := range enum {
				vals = append(vals, "`"+str(v)+"`")
			}
			b.WriteString("取值：" + strings.Join(vals, "、") + "\n\n")
		}
		renderProperties(b, s)
	}
}

// ---- 小工具 ----

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func sliceOf(v any) []any {
	s, _ := v.([]any)
	return s
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// resolveRef 顺着 `#/a/b/c` 形式的本地引用取出被引用的节点。
// 复用的响应（如 Unauthorized）全靠它才拿得到 description，否则表格里会是空的。
func resolveRef(node map[string]any) map[string]any {
	ref := str(node["$ref"])
	if ref == "" || !strings.HasPrefix(ref, "#/") {
		return node
	}
	cur := specDoc
	for _, seg := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		next, ok := cur[seg]
		if !ok {
			return node
		}
		cur = mapOf(next)
	}
	return cur
}

// schemaTypeName 把 schema 压成一个简短的类型名，$ref 直接取末段。
func schemaTypeName(s map[string]any) string {
	if len(s) == 0 {
		return "—"
	}
	if ref := str(s["$ref"]); ref != "" {
		parts := strings.Split(ref, "/")
		return parts[len(parts)-1]
	}
	if len(sliceOf(s["allOf"])) > 0 {
		return "object"
	}
	t := str(s["type"])
	if t == "array" {
		return schemaTypeName(mapOf(s["items"])) + "[]"
	}
	if t == "" {
		return "object"
	}
	if enum := sliceOf(s["enum"]); len(enum) > 0 {
		return t + " (枚举)"
	}
	return t
}

// jsonSchemaOf 取 requestBody 里 application/json 的 schema。
func jsonSchemaOf(rb map[string]any) map[string]any {
	content := mapOf(rb["content"])
	return mapOf(mapOf(content["application/json"])["schema"])
}

// firstExample 取 requestBody 的示例，优先 example，其次 examples 的第一项。
func firstExample(rb map[string]any) string {
	js := mapOf(mapOf(rb["content"])["application/json"])
	if ex, ok := js["example"]; ok {
		return toYAMLish(ex)
	}
	examples := mapOf(js["examples"])
	keys := make([]string, 0, len(examples))
	for k := range examples {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return toYAMLish(mapOf(examples[keys[0]])["value"])
}

// toYAMLish 示例块用 JSON 呈现更贴近实际请求体。
func toYAMLish(v any) string {
	b, err := jsonIndent(v)
	if err != nil {
		out, _ := yaml.Marshal(v)
		return strings.TrimSpace(string(out))
	}
	return b
}

// inlineText 把多行描述压成一行，避免撑破 Markdown 表格。
func inlineText(s string) string {
	s = strings.TrimSpace(dedent(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "|", "\\|")
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		kept = append(kept, ln)
	}
	joined := strings.Join(kept, " ")
	// 描述里带表格或列表时整段塞进单元格没有可读性，截断并指向规范原文。
	if len(joined) > 400 {
		return joined[:400] + "…（完整说明见 OpenAPI 规范）"
	}
	return joined
}

// dedent 去掉 YAML 块标量残留的统一缩进。
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	indent := -1
	for _, ln := range lines {
		trimmed := strings.TrimLeft(ln, " ")
		if trimmed == "" {
			continue
		}
		n := len(ln) - len(trimmed)
		if indent < 0 || n < indent {
			indent = n
		}
	}
	if indent <= 0 {
		return s
	}
	for i, ln := range lines {
		if len(ln) >= indent {
			lines[i] = ln[indent:]
		} else {
			lines[i] = strings.TrimLeft(ln, " ")
		}
	}
	return strings.Join(lines, "\n")
}

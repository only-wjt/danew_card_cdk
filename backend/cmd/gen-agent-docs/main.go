// Command gen-agent-docs 把代理 API 的 OpenAPI 规范渲染成 docs/agent-api-zh.md。
//
// 改完 backend/internal/apidocs/agent-openapi.yaml 后，在 backend/ 下执行：
//
//	go run ./cmd/gen-agent-docs
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danew/cdk-recharge-system/internal/apidocs"
)

func main() {
	out := flag.String("o", filepath.Join("..", "docs", "agent-api-zh.md"), "输出文件路径")
	flag.Parse()

	md, err := apidocs.RenderMarkdown()
	if err != nil {
		fmt.Fprintf(os.Stderr, "渲染失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入 %s 失败: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Printf("已生成 %s（%d 字节）\n", *out, len(md))
}

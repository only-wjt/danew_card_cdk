package provider

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// NewSiteCode 生成本站对外码：DN-<8hex>-<8hex>-<8hex>。
func NewSiteCode() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s%s-%s-%s", SiteCodePrefix, h[0:8], h[8:16], h[16:24]), nil
}

// IsSiteCode 是否为本站统一码（非 legacy / 非白号导入行）。
func IsSiteCode(code string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(code)), strings.ToUpper(SiteCodePrefix))
}

// SiteCodePrefixOf 列表展示用前缀（约 14 字符）。
func SiteCodePrefixOf(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if len(code) <= 14 {
		return code
	}
	return code[:14]
}

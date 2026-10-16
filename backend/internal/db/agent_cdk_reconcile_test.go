package db

import "testing"

// 终态 → 库存状态映射。skipped 表示账号已是目标套餐（上游未扣款、码未消耗），
// 曾经和 success 一起走 ConsumeAgentCDK，把代理的码白烧掉。
func TestReconcileCDKAfterItemTerminalStatuses(t *testing.T) {
	cases := []struct {
		itemStatus string
		want       string
	}{
		{"success", "consumed"},
		{"skipped", "unused"},
		{"failed", "unused"},
		{"unknown", "reserved"},
	}
	for _, c := range cases {
		t.Run(c.itemStatus, func(t *testing.T) {
			openTestDB(t)
			code := "CDK-RECON-" + c.itemStatus
			if err := SaveCardplatformCDKCode(7001, code, "CDK-RECON", "plus", 100); err != nil {
				t.Fatalf("save cdk: %v", err)
			}
			// 代理提交时码是 reserved（走 CreateAdminRechargeBatch 里的真实占位）；
			// 明细落终态后再按上表流转
			reserveViaBatch(t, code, "recon-"+c.itemStatus, c.itemStatus)
			if got := cdkStatusByCode(code); got != "reserved" {
				t.Fatalf("reserve via batch: status=%q, want reserved", got)
			}
			ReconcileCDKAfterItemTerminal(code, c.itemStatus)
			row, ok := LookupStoredCDKDetail(code)
			if !ok {
				t.Fatalf("%s: cdk missing", c.itemStatus)
			}
			if row.Status != c.want {
				t.Fatalf("%s: status = %q, want %q", c.itemStatus, row.Status, c.want)
			}
		})
	}
}

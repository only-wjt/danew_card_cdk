package db

import (
	"fmt"
	"strings"
)

// SiteCDKRow 本站码行（对外可见 code；上游在 binding 表）。
type SiteCDKRow struct {
	ID           int64
	Code         string
	CodePrefix   string
	Plan         string
	CodeKind     string
	IssueStatus  string
	DualEligible bool
	Status       string
}

// CreatePendingSiteCDK 插入 pending 本站码（尚未双发完成，不可复制给代理）。
func CreatePendingSiteCDK(code, plan string, dualEligible bool, feeMinor int64) (SiteCDKRow, error) {
	if DB == nil {
		return SiteCDKRow{}, fmt.Errorf("db not ready")
	}
	code = strings.TrimSpace(code)
	plan = strings.TrimSpace(plan)
	if code == "" || plan == "" {
		return SiteCDKRow{}, fmt.Errorf("code and plan required")
	}
	prefix := code
	if len(prefix) > 14 {
		prefix = prefix[:14]
	}
	dual := 0
	if dualEligible {
		dual = 1
	}
	res, err := DB.Exec(`
		INSERT INTO cardplatform_cdk_codes
		(upstream_id, code, code_prefix, plan, fee_amount_minor, status, code_kind, issue_status, dual_eligible, created_at)
		VALUES (0, ?, ?, ?, ?, 'unused', 'site', ?, ?, CURRENT_TIMESTAMP)
	`, code, prefix, plan, feeMinor, IssueStatusPending, dual)
	if err != nil {
		return SiteCDKRow{}, err
	}
	id, _ := res.LastInsertId()
	return SiteCDKRow{
		ID: id, Code: code, CodePrefix: prefix, Plan: plan,
		CodeKind: CodeKindSite, IssueStatus: IssueStatusPending, DualEligible: dualEligible,
		Status: "unused",
	}, nil
}

// ActivateSiteCDK 双绑完成后激活本站码。
func ActivateSiteCDK(siteCodeID int64, feeMinor int64) error {
	if DB == nil || siteCodeID <= 0 {
		return fmt.Errorf("invalid site code id")
	}
	_, err := DB.Exec(`
		UPDATE cardplatform_cdk_codes
		SET issue_status = ?, status = 'unused',
		    fee_amount_minor = CASE WHEN ? > 0 THEN ? ELSE fee_amount_minor END
		WHERE id = ? AND code_kind = 'site'
	`, IssueStatusActive, feeMinor, feeMinor, siteCodeID)
	return err
}

// FailSiteCDK 双发失败，作废 pending 行。
func FailSiteCDK(siteCodeID int64) error {
	if DB == nil || siteCodeID <= 0 {
		return nil
	}
	_, err := DB.Exec(`
		UPDATE cardplatform_cdk_codes SET issue_status = ?, status = 'disabled'
		WHERE id = ? AND code_kind = 'site'
	`, IssueStatusFailed, siteCodeID)
	return err
}

func UpdateCardplatformCDKStatusByRowID(id int64, status string) error {
	if DB == nil || id <= 0 {
		return fmt.Errorf("invalid cdk row id")
	}
	_, err := DB.Exec(`UPDATE cardplatform_cdk_codes SET status = ? WHERE id = ?`,
		strings.TrimSpace(status), id)
	return err
}

// GetSiteCDKByCode 按本站码查行。
func GetSiteCDKByCode(code string) (SiteCDKRow, bool) {
	if DB == nil {
		return SiteCDKRow{}, false
	}
	var row SiteCDKRow
	var dual int
	err := DB.QueryRow(`
		SELECT id, code, COALESCE(code_prefix,''), COALESCE(plan,''), COALESCE(code_kind,''),
		       COALESCE(issue_status,''), COALESCE(dual_eligible,0), COALESCE(status,'')
		FROM cardplatform_cdk_codes
		WHERE upper(trim(code)) = upper(trim(?))
		LIMIT 1
	`, code).Scan(&row.ID, &row.Code, &row.CodePrefix, &row.Plan, &row.CodeKind,
		&row.IssueStatus, &dual, &row.Status)
	if err != nil {
		return SiteCDKRow{}, false
	}
	row.DualEligible = dual != 0
	return row, true
}

// CardPlatformAccountIDForCode 返回一张码实际所属/已选定的卡台账户。
// legacy 没有 binding，固定归主台；DN- 优先 active binding，其次已履约账户。
func CardPlatformAccountIDForCode(code string) int64 {
	row, ok := GetSiteCDKByCode(code)
	if !ok || row.CodeKind != CodeKindSite {
		acc, err := CardPlatformAccountForLegacyCode(code)
		if err == nil {
			return acc.ID
		}
		return 0
	}
	var accountID int64
	_ = DB.QueryRow(`
		SELECT COALESCE(b.account_id, c.fulfilled_account_id, 0)
		FROM cardplatform_cdk_codes c
		LEFT JOIN site_cdk_bindings b ON b.id = c.active_binding_id
		WHERE c.id = ?
	`, row.ID).Scan(&accountID)
	return accountID
}

package db

import (
	"errors"
	"testing"
)

func TestPreviewTokensAppendOnlyAndBackfillOnce(t *testing.T) {
	openTestDB(t)
	// 首次建表回填：上线前预览过的 token 还能用（RT: 伪行不回填）。
	if _, err := DB.Exec(`DROP TABLE cdk_preview_tokens`); err != nil {
		t.Fatal(err)
	}
	if err := BindCDKRedemptionToken("old-code", "tok-old"); err != nil {
		t.Fatal(err)
	}
	if err := BindCDKSession("", "tok-rt", `{"sessionToken":"x"}`); err != nil {
		t.Fatal(err)
	}
	if err := migrateCardnet(); err != nil {
		t.Fatal(err)
	}
	if code, err := PreviewTokenCode("tok-old"); err != nil || code != "OLD-CODE" {
		t.Fatalf("backfill: %q %v", code, err)
	}
	if _, err := PreviewTokenCode("tok-rt"); !errors.Is(err, ErrCardnetNotFound) {
		t.Fatalf("RT: rows must not be backfilled: %v", err)
	}

	// 只增不改：同一个 token 不能改挂到别的码。之后的绑定改动也不再回填。
	if err := RecordPreviewToken("code-a", "tok-1"); err != nil {
		t.Fatal(err)
	}
	if err := RecordPreviewToken("code-b", "tok-1"); err != nil {
		t.Fatal(err)
	}
	if err := BindCDKRedemptionToken("code-c", "tok-new"); err != nil {
		t.Fatal(err)
	}
	if err := migrateCardnet(); err != nil {
		t.Fatal(err)
	}
	if code, _ := PreviewTokenCode("tok-1"); code != "CODE-A" {
		t.Fatalf("token was remapped to %q", code)
	}
	if _, err := PreviewTokenCode("tok-new"); !errors.Is(err, ErrCardnetNotFound) {
		t.Fatalf("bindings after the first migration must not be imported: %v", err)
	}
}

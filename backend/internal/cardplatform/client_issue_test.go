package cardplatform

import "testing"

func TestMaxIssueCountMatchesBatchLimit(t *testing.T) {
	if MaxIssueCount != 200 {
		t.Fatalf("MaxIssueCount=%d want 200", MaxIssueCount)
	}
}

func TestCDKListItemFullCodeText(t *testing.T) {
	if (CDKListItem{CodePrefix: "ZC-AAAAAAAAAAAA"}).FullCodeText() != "" {
		t.Fatal("prefix-only must not look like a full code")
	}
	full := "ZC-AAAAAAAAAAAA-BBBBBBBBBBBB-CCCCCCCCCCCC"
	if got := (CDKListItem{Code: full}).FullCodeText(); got != full {
		t.Fatalf("code field: got %q", got)
	}
	if got := (CDKListItem{FullCode: full, Code: "ZC-AAAAAAAAAAAA"}).FullCodeText(); got != full {
		t.Fatalf("full_code should win: got %q", got)
	}
}

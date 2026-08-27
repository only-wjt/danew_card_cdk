package provider

import "testing"

func TestNewSiteCodeFormat(t *testing.T) {
	code, err := NewSiteCode()
	if err != nil {
		t.Fatal(err)
	}
	if !IsSiteCode(code) {
		t.Fatalf("not site code: %q", code)
	}
	if len(code) < 20 {
		t.Fatalf("too short: %q", code)
	}
	prefix := SiteCodePrefixOf(code)
	if len(prefix) > 14 {
		t.Fatalf("prefix too long: %q", prefix)
	}
}

func TestIsSiteCodeRejectsLegacy(t *testing.T) {
	if IsSiteCode("GPTD-ABCD-1234-5678") {
		t.Fatal("legacy should not be site code")
	}
	if !IsSiteCode("DN-A1B2C3D4-E5F60718-90ABCDEF") {
		t.Fatal("DN- should be site code")
	}
}

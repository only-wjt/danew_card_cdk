package handler

import (
	"reflect"
	"testing"
)

func TestNormalizeLookupCodes(t *testing.T) {
	got := normalizeLookupCodes(append(
		[]string{"sxc-aaaa-bbbb-cccc-dddd", "SXC-AAAA-BBBB-CCCC-DDDD", "ab"},
		splitLookupText("sxc-eeee-ffff-gggg-hhhh\nsxc-iiii-jjjj-kkkk-llll,sxc-eeee-ffff-gggg-hhhh")...,
	))
	want := []string{
		"SXC-AAAA-BBBB-CCCC-DDDD",
		"SXC-EEEE-FFFF-GGGG-HHHH",
		"SXC-IIII-JJJJ-KKKK-LLLL",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeLookupCodes = %#v, want %#v", got, want)
	}
}

func TestApplyLookupFailure(t *testing.T) {
	reusable := cdkLookupResult{CDKCode: "SXC-1"}
	applyLookupFailure(&reusable, "Session 无效", true)
	if reusable.Status != "failed" || !reusable.CanResubmit || reusable.Used {
		t.Fatalf("reusable: %+v", reusable)
	}
	if reusable.Notes != "Session 无效" {
		t.Fatalf("notes=%q", reusable.Notes)
	}

	locked := cdkLookupResult{CDKCode: "SXC-2"}
	applyLookupFailure(&locked, "", false)
	if locked.Status != "failed" || locked.CanResubmit || locked.Used {
		t.Fatalf("locked: %+v", locked)
	}
}

func TestSplitLookupTextEmpty(t *testing.T) {
	if got := splitLookupText("  \n"); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

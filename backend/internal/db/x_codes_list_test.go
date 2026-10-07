package db

import "testing"

func TestListXRecordsFiltersBeforeLimit(t *testing.T) {
	openTestDB(t)
	insert := func(code, status, note string) {
		t.Helper()
		if _, err := DB.Exec(`
			INSERT INTO x_codes (code, plan, channel, account_id, status, note)
			VALUES (?, 'premium_3m', 'x_cdk', 1, ?, ?)
		`, code, status, note); err != nil {
			t.Fatal(err)
		}
	}
	insert("DNX-OLD-UNUSED", "unused", "findme")
	for _, code := range []string{"DNX-NEW-1", "DNX-NEW-2", "DNX-NEW-3"} {
		insert(code, "completed", "")
	}
	rows, total, err := ListXRecords("unused", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0]["code"] != "DNX-OLD-UNUSED" {
		t.Fatalf("unused filter missed older code: total=%d rows=%v", total, rows)
	}
	rows, total, err = ListXRecords("all", "findme", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0]["code"] != "DNX-OLD-UNUSED" {
		t.Fatalf("search missed older code: total=%d rows=%v", total, rows)
	}
	_, total, err = ListXRecords("all", "", "premium_3m", 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("plan total = %d, want 4", total)
	}
}

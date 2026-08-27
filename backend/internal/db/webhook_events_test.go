package db

import "testing"

func TestWebhookEventsAreScopedByAccount(t *testing.T) {
	openTestDB(t)
	if err := InsertWebhookEvent(1, "gpt_direct.completed", "gpt_direct.completed|order|same", `{"order_id":"same"}`); err != nil {
		t.Fatal(err)
	}
	if err := InsertWebhookEvent(2, "gpt_direct.completed", "gpt_direct.completed|order|same", `{"order_id":"same"}`); err != nil {
		t.Fatal(err)
	}
	all, err := ListWebhookEvents(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 events, got %d", len(all))
	}
	a, err := ListWebhookEvents(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || a[0].AccountID != 1 || a[0].IdemKey != "gpt_direct.completed|order|same" {
		t.Fatalf("account 1 scoped list = %+v", a)
	}
	b, err := ListWebhookEvents(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 1 || b[0].AccountID != 2 {
		t.Fatalf("account 2 scoped list = %+v", b)
	}
}

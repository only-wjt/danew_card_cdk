package avanfinity

import "testing"

func TestPickExistingCardFollowsOrderAndSkipsDisabled(t *testing.T) {
	cards := []Card{
		{ID: 1, Status: "active"},
		{ID: 2, Status: "frozen"},
		{ID: 3, Status: "active"},
	}
	order := []CardPref{{ID: 2, Enabled: true}, {ID: 1, Enabled: false}, {ID: 3, Enabled: true}}
	got, ok := PickExistingCard(cards, order)
	if !ok || got.ID != 3 {
		t.Fatalf("want card 3, got %+v ok=%v", got, ok)
	}
}

func TestPickExistingCardUsesFirstUsableWhenOrderEmpty(t *testing.T) {
	cards := []Card{{ID: 9, Status: "closed"}, {ID: 4, Status: "active"}}
	got, ok := PickExistingCard(cards, nil)
	if !ok || got.ID != 4 {
		t.Fatalf("want card 4, got %+v ok=%v", got, ok)
	}
}

func TestPayFieldsExistingStopsWithoutFallback(t *testing.T) {
	_, err := PayFields(PayPlan{Mode: "existing"}, []Card{{ID: 1, Status: "frozen"}})
	if err == nil {
		t.Fatal("expected stop when no usable card")
	}
}

func TestPayFieldsExistingFallsBackToNewCard(t *testing.T) {
	fields, err := PayFields(PayPlan{
		Mode: "existing", Fallback: true, Product: "one", FirstName: "San", LastName: "Zhang",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	auto, _ := fields["autoCard"].(map[string]string)
	if auto["productCode"] != "one" || fields["cardId"] != nil {
		t.Fatalf("fields %+v", fields)
	}
}

func TestPayFieldsFixedAndNew(t *testing.T) {
	fixed, err := PayFields(PayPlan{Mode: "fixed", CardID: 8}, nil)
	if err != nil || fixed["cardId"] != int64(8) {
		t.Fatalf("fixed %+v err=%v", fixed, err)
	}
	_, err = PayFields(PayPlan{Mode: "new"}, nil)
	if err == nil {
		t.Fatal("new card without holder should fail")
	}
}

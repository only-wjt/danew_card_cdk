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

func TestCandidateCardsSkipLowBalanceAndLimitTries(t *testing.T) {
	cards := []Card{
		{ID: 1, Status: "active", Balance: "0.10"},
		{ID: 2, Status: "active", Balance: "20"},
		{ID: 3, Status: "active", Balance: "8"},
		{ID: 4, Status: "active", Balance: "6"},
	}
	ids := CandidateCardIDs(PayPlan{Mode: "existing", ExtraTries: 1, MinBalance: "1", Order: []CardPref{
		{ID: 1, Enabled: true}, {ID: 2, Enabled: true}, {ID: 3, Enabled: true}, {ID: 4, Enabled: true},
	}}, cards)
	if len(ids) != 2 || ids[0] != 2 || ids[1] != 3 {
		t.Fatalf("want 2 then 3, got %v", ids)
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

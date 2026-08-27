package db

import "testing"

func TestAccountCardSelectionAndHealthAreIsolated(t *testing.T) {
	openTestDB(t)
	aID, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "A", Protocol: AccountProtocolSpaceXLegacy, SiteBase: "https://a.example",
		CredSecret: "sk-a", Status: "active", Priority: 10, IsPrimaryDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	bID, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "B", Protocol: AccountProtocolSpaceXLegacy, SiteBase: "https://b.example",
		CredSecret: "sk-b", Status: "active", Priority: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetCardSelectionRulesForAccount(aID, []CardSelectionRule{
		{PlanKey: "A-PRODUCT", Channel: "one", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetCardSelectionRulesForAccount(bID, []CardSelectionRule{
		{PlanKey: "B-PRODUCT", Channel: "two", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	_, _, aPref := PreferredCardSelectionForAccount(aID)
	_, _, bPref := PreferredCardSelectionForAccount(bID)
	if aPref != "A-PRODUCT" || bPref != "B-PRODUCT" {
		t.Fatalf("preferences leaked: A=%q B=%q", aPref, bPref)
	}

	if err := UpsertCardBlock(CardBlockEntry{AccountID: aID, CardID: 77, Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	aBlocked, err := IsCardBlocked(aID, 77)
	if err != nil {
		t.Fatal(err)
	}
	bBlocked, err := IsCardBlocked(bID, 77)
	if err != nil {
		t.Fatal(err)
	}
	if !aBlocked || bBlocked {
		t.Fatalf("same upstream card id leaked across accounts: A=%v B=%v", aBlocked, bBlocked)
	}

	cID, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "C", Protocol: AccountProtocolSpaceXLegacy, SiteBase: "https://c.example",
		CredSecret: "sk-c", Status: "active", Priority: 5, IsPrimaryDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cRules, err := GetCardSelectionRulesForAccount(cID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cRules) != 0 {
		t.Fatalf("changing primary must not reseed legacy rules into C: %+v", cRules)
	}
	legacy, err := LegacyCardPlatformAccount()
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ID != aID {
		t.Fatalf("legacy owner changed with primary: got=%d want=%d", legacy.ID, aID)
	}
	if err := SaveCardplatformCDKCodeForAccount(991, "GPTD-NEW-ON-C", "GPTD-NEW", "plus", 100, "unused", cID); err != nil {
		t.Fatal(err)
	}
	owner, err := CardPlatformAccountForLegacyCode("GPTD-NEW-ON-C")
	if err != nil {
		t.Fatal(err)
	}
	if owner.ID != cID {
		t.Fatalf("new native code not pinned to issuing primary: got=%d want=%d", owner.ID, cID)
	}
}

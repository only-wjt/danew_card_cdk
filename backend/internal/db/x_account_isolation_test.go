package db

import "testing"

func TestXAccountStaysOutOfOpenAIIssue(t *testing.T) {
	openTestDB(t)
	openAI, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "主台", Protocol: AccountProtocolSpaceXLegacy, SiteBase: "https://openai.example",
		CredSecret: "sk-openai", Status: "active", Priority: 10, IsPrimaryDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	xID, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "X直充", Protocol: AccountProtocolAvanfinityAPIv1, SiteBase: "https://x.example",
		CredPublic: "app_test", CredSecret: "sk-x", Capabilities: "x_direct",
		Status: "active", Priority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "混用", Protocol: AccountProtocolAvanfinityAPIv1, SiteBase: "https://x.example",
		CredPublic: "app_2", CredSecret: "sk-x2", Capabilities: "openai",
		Status: "active", Priority: 2,
	}); err == nil {
		t.Fatal("X account must not take the openai capability")
	}
	if _, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "抢主台", Protocol: AccountProtocolAvanfinityAPIv1, SiteBase: "https://x.example",
		CredPublic: "app_3", CredSecret: "sk-x3", Capabilities: "x_cdk",
		Status: "active", IsPrimaryDefault: true,
	}); err == nil {
		t.Fatal("X account must not become primary")
	}

	issuers, err := ActiveDualIssueAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(issuers) != 1 || issuers[0].ID != openAI {
		t.Fatalf("issuers = %+v, want only openai %d", issuers, openAI)
	}
	primary, err := PrimaryCardPlatformAccount()
	if err != nil || primary.ID != openAI {
		t.Fatalf("primary = %+v err=%v", primary, err)
	}
	xAcc, err := GetCardPlatformAccount(xID)
	if err != nil {
		t.Fatal(err)
	}
	if xAcc.IsPrimaryDefault || AccountServesOpenAI(xAcc) {
		t.Fatalf("x account leaked into openai: %+v", xAcc)
	}
	if xAcc.Capabilities != CapXDirect {
		t.Fatalf("capabilities = %q", xAcc.Capabilities)
	}
}

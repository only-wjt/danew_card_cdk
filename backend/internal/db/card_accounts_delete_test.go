package db

import "testing"

func TestDeleteCardPlatformAccountGuards(t *testing.T) {
	openTestDB(t)
	primary, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "A", Protocol: AccountProtocolSpaceXLegacy, SiteBase: "https://a.example",
		CredSecret: "sk-a", Status: "active", Priority: 10, IsPrimaryDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	xID, err := UpsertCardPlatformAccount(CardPlatformAccount{
		Name: "Avan · X", Protocol: AccountProtocolAvanfinityAPIv1, Capabilities: CapXCDK,
		SiteBase: "https://x.example", CredPublic: "app", CredSecret: "sk", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteCardPlatformAccount(primary); err == nil {
		t.Fatal("primary should not be deletable")
	}

	if err := SaveXChannel(XChannel{Channel: XChannelCDK, AccountID: xID, Enabled: true, AutoCard: true,
		AutoCardProduct: "P1", AutoCardFirstName: "San", AutoCardLastName: "Zhang"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCardPlatformAccount(xID); err == nil {
		t.Fatal("account with enabled X channel should not be deletable")
	}
	if err := SaveXChannel(XChannel{Channel: XChannelCDK, AccountID: xID, Enabled: false, AutoCard: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`INSERT INTO x_codes (code, plan, channel, account_id, status) VALUES ('DNX-T1','premium_3m','x_cdk',?,'unused')`, xID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCardPlatformAccount(xID); err == nil {
		t.Fatal("account with unused X codes should not be deletable")
	}
	if _, err := DB.Exec(`UPDATE x_codes SET status = 'completed' WHERE code = 'DNX-T1'`); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCardPlatformAccount(xID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetCardPlatformAccount(xID); err == nil {
		t.Fatal("account still exists")
	}
	var bound int64
	_ = DB.QueryRow(`SELECT account_id FROM x_channels WHERE channel = ?`, XChannelCDK).Scan(&bound)
	if bound != 0 {
		t.Fatalf("x channel still points at deleted account: %d", bound)
	}
}

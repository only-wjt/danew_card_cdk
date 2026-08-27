package db

import "testing"

func TestSetCardPlatformWebhookSecretPerAccount(t *testing.T) {
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
	if err := SetCardPlatformWebhookSecret(aID, "whsec_aaa"); err != nil {
		t.Fatal(err)
	}
	if err := SetCardPlatformWebhookSecret(bID, "whsec_bbb"); err != nil {
		t.Fatal(err)
	}
	if err := SetCardPlatformWebhookSecret(bID, ""); err == nil {
		t.Fatal("empty secret should be rejected")
	}
	a, err := GetCardPlatformAccount(aID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GetCardPlatformAccount(bID)
	if err != nil {
		t.Fatal(err)
	}
	if a.WebhookSecret != "whsec_aaa" || b.WebhookSecret != "whsec_bbb" {
		t.Fatalf("secrets leaked or overwritten: A=%q B=%q", a.WebhookSecret, b.WebhookSecret)
	}
	legacy, _ := GetSetting("webhook_secret")
	if legacy != "whsec_aaa" {
		t.Fatalf("primary webhook should mirror to site_settings, got %q", legacy)
	}
}

func TestSetCardPlatformWebhookPathIsEditableAndUnique(t *testing.T) {
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
	if _, err := NormalizeAccountWebhookPath("/avanfinity"); err == nil {
		t.Fatal("path outside /api/v1/webhooks/ should be rejected")
	}
	if err := SetCardPlatformWebhookPath(aID, "https://cdk.example/api/v1/webhooks/zavacard"); err != nil {
		t.Fatal(err)
	}
	if err := SetCardPlatformWebhookPath(bID, "https://cdk.example/api/v1/webhooks/zavacard"); err == nil {
		t.Fatal("duplicate path should be rejected")
	}
	a, err := GetCardPlatformAccount(aID)
	if err != nil {
		t.Fatal(err)
	}
	if a.WebhookPath != "/api/v1/webhooks/zavacard" {
		t.Fatalf("saved path = %q", a.WebhookPath)
	}
	found, err := FindCardPlatformAccountByWebhookPath("/api/v1/webhooks/zavacard")
	if err != nil || found.ID != aID {
		t.Fatalf("lookup custom path: %+v %v", found, err)
	}
	found, err = FindCardPlatformAccountByWebhookPath(DefaultAccountWebhookPath(bID))
	if err != nil || found.ID != bID {
		t.Fatalf("lookup default path: %+v %v", found, err)
	}
}

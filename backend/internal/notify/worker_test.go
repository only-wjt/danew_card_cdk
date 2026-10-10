package notify

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
)

func TestBoundedDrainAndSavedConfigRecovery(t *testing.T) {
	queueDB(t)
	// All env credentials remain empty. Simulate settings becoming available
	// after a previously unconfigured attempt, with no worker restart needed.
	if err := EnqueueRedeemed("configured-later", "X", "P", "u", ""); err != nil {
		t.Fatal(err)
	}
	drain(context.Background())
	if status, _, _ := state(t, "configured-later"); status != "retry" {
		t.Fatal(status)
	}
	if err := db.SetSetting("telegram_token", fakeToken); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("telegram_chat_id", "FAKE_CHAT"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, `{"ok":true}`), nil
	})
	due(t, "configured-later")
	for i := 0; i < maxDrain+2; i++ {
		if err := EnqueueRedeemed(fmt.Sprintf("bounded-%d", i), "X", "P", "u", ""); err != nil {
			t.Fatal(err)
		}
	}
	drain(context.Background())
	if calls != maxDrain {
		t.Fatalf("unbounded drain: %d sends", calls)
	}
	if status, _, _ := state(t, "configured-later"); status != "sent" {
		t.Fatal(status)
	}
	drain(context.Background())
	if calls != maxDrain+3 {
		t.Fatalf("remaining queue not drained: %d sends", calls)
	}
}

func TestEnqueueChatGPTRedeemedAliases(t *testing.T) {
	queueDB(t)
	// A configured sender still must not send HTTP from an enqueue wrapper.
	configure(t)
	if err := EnqueueChatGPTRedeemedAliases([]string{"gpt-order", "gpt-redemption"}, " <Plus> ", " person&test@example.test ", " CDK&1 ", ""); err != nil {
		t.Fatal(err)
	}
	var original string
	if err := db.DB.QueryRow(`SELECT text FROM telegram_notifications WHERE business_key = 'gpt-order'`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"ChatGPT", "套餐: &lt;Plus&gt;", "账号: person&amp;test@example.test", "卡密: <code>CDK&amp;1</code>", "地区: 待同步"} {
		if !strings.Contains(original, part) {
			t.Fatalf("formatted message missing %q", part)
		}
	}
	if err := EnqueueChatGPTRedeemedAliases([]string{"gpt-redemption", "gpt-poll"}, "different", "different", "different", "US"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("notification count=%d err=%v", count, err)
	}
	var retained string
	if err := db.DB.QueryRow(`SELECT text FROM telegram_notifications WHERE business_key = 'gpt-order'`).Scan(&retained); err != nil || retained != original {
		t.Fatalf("original message changed: err=%v", err)
	}
	if err := EnqueueChatGPTRedeemedAliases(nil, "Plus", "email", "code", "region"); err == nil {
		t.Fatal("expected DB validation error to propagate")
	}
}

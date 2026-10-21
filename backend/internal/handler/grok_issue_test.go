package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// Grok 发码：拉 Grok 档位表、对卡台显式发 grok_ 前缀档位、地区固定 US。
func TestGrokIssueUsesGrokCatalogueAndPrefixedPlan(t *testing.T) {
	openHandlerTestDB(t)
	issued := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v1/gpt-direct/plans":
			if r.URL.Query().Get("product") != "grok" {
				t.Error("Grok issue fetched another catalogue")
			}
			w.Write([]byte(`{"code":0,"data":{"version":1,"plans":{"grok_premium_monthly":{"enabled":true,"currency":"USD"}},"registry":[{"key":"premium_monthly","label":"Grok Premium 月付"}]}}`))
		case "/openapi/v1/gpt-direct/cdks":
			var b map[string]any
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
			}
			if b["plan"] != "grok_premium_monthly" || b["payment_country"] != "US" {
				t.Errorf("wrong Grok issue parameters %v", b)
			}
			issued++
			w.Write([]byte(`{"code":0,"data":{"requested":1,"issued":[{"id":1,"code":"ZC-FAKE-TEST-GROK-CODE","plan":"premium_monthly","payment_country":"US"}]}}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	// 本站 Grok 码只走主台（与 X 同理），主台必须是支持付款地区的卡台。
	gptTestSQL(t, `INSERT INTO card_platform_accounts
		(id,name,site_base,protocol,cred_secret,capabilities,status,is_primary_default,priority)
		VALUES (7,'zovo',?,'spacexcard-legacy','fixture-key','openai','active',1,1)`, s.URL)
	if e := db.SetSetting("card_api_base", s.URL); e != nil {
		t.Fatal(e)
	}
	if e := db.SetSetting("card_api_key", "fixture-key"); e != nil {
		t.Fatal(e)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/issue", CardPlatformIssueCDKs)
	for _, body := range []string{
		`{"plan":"premium_monthly","product":"grok","count":1,"funding_confirmed":true,"payment_country":"PH"}`,
		`{"plan":"grok_premium_monthly","count":1,"funding_confirmed":true}`, // 老前端不传 product：按带前缀的档位推断
		`{"plan":"grok_monthly","count":1,"funding_confirmed":true}`,         // 改名前的旧键：换成 premium_monthly
	} {
		req := httptest.NewRequest("POST", "/issue", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("Grok issue failed %d %s", w.Code, w.Body.String())
		}
	}
	if issued != 3 {
		t.Fatalf("issued %d", issued)
	}
}

func TestIssueProductKeepsGPTDefault(t *testing.T) {
	cases := map[[2]string]string{
		{"", "plus"}: "gpt", {"", "pro_20x_renew"}: "gpt", {"", "basic_monthly"}: "x",
		{"", "heavy_yearly"}: "grok", {"gpt", "plus"}: "gpt", {"bogus", "plus"}: "gpt",
		// 裸 premium_* 同时是 X 和 Grok 的键：没有 product 时归 X；带 grok_ 前缀才是 Grok
		{"", "premium_monthly"}: "x", {"", "grok_premium_monthly"}: "grok", {"", "super_monthly"}: "grok",
		{"grok", "premium_monthly"}: "grok",
	}
	for in, want := range cases {
		if got := issueProduct(in[0], in[1]); got != want {
			t.Fatalf("issueProduct(%q,%q)=%s want %s", in[0], in[1], got, want)
		}
	}
}

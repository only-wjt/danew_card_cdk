package cardplatform

import (
	"encoding/json"
	"errors"
	"strings"
)

func XPaymentCountry(country string) (string, error) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		country = "JP"
	}
	switch country {
	case "US", "JP", "PH", "NG", "TR", "EG":
		return country, nil
	}
	return "", errors.New("X supports US, JP, PH, NG, TR and EG only")
}

func IsXPremiumPlan(plan string) bool {
	plan = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(plan)), "x_")
	switch plan {
	case "basic_monthly", "basic_yearly", "premium_monthly", "premium_yearly", "premium_plus_monthly", "premium_plus_yearly":
		return true
	}
	return false
}

// X cookies are execution credentials, not reusable ChatGPT invoice sessions.
func IsXPremiumCredential(raw string) bool {
	var x map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &x) != nil {
		return false
	}
	return len(x["auth_token"]) > 0 || (len(x["cookieHeader"]) > 0 && (len(x["billing_email"]) > 0 || len(x["billingEmail"]) > 0))
}

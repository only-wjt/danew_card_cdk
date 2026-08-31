package cardplatform

import "testing"

func TestCanonicalCardIssuer(t *testing.T) {
	if CanonicalCardIssuer("ch1") != "one" || CanonicalCardIssuer("CH4") != "four" {
		t.Fatal("CDK historical channel aliases must map to cardplatform issuers")
	}
}

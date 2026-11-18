package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOverviewSharedCredentialAndAdoptionBounds(t *testing.T) {
	for _, token := range []string{"", "a b", "a\nb", "a\x00b", strings.Repeat("x", 16385)} {
		if _, err := observationInput([]byte(token)); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	for _, token := range []string{"token", strings.Repeat("<", 16384)} {
		data, err := observationInput([]byte(token))
		var fields map[string]string
		if err != nil || len(data) > observationInputLimit ||
			json.Unmarshal(data, &fields) != nil || len(fields) != 1 ||
			fields["token"] != token {
			t.Fatal("valid bounded token altered")
		}
	}
	for _, host := range []*sharedHost{nil, {MemberID: 2},
		{MemberID: 1, AdoptedRepositoryIDs: []int64{0}},
		{MemberID: 1, AdoptedRepositoryIDs: make([]int64, 33)}} {
		if ids, partial := overviewAdoptedIDs(host, 1); !partial || len(ids) != 0 {
			t.Fatal("unverified adoption represented as complete")
		}
	}
}

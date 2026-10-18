package broker

import (
	"context"
	"strings"
	"testing"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

func TestAtomicPublicAndPrivateTopicDecisions(t *testing.T) {
	public := "market.trades.0x" + strings.Repeat("1", 40)
	private := "private.portfolio.0x" + strings.Repeat("2", 40)
	var set TopicSet
	b := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: []string{public, private}, Origin: "https://app.example", Endpoint: "/v2/stream"}
	b.FilterDigest = sharedauth.StreamFilterDigest(b.Topics)
	decisions, err := set.Replace(context.Background(), sharedauth.StreamPrincipal{}, b, nil)
	if err != nil || len(decisions) != 2 {
		t.Fatal(decisions, err)
	}
	accepted := set.Topics()
	if len(accepted) != 1 || accepted[0] != public {
		t.Fatal(accepted)
	}
	b.Topics = []string{"*"}
	b.FilterDigest = sharedauth.StreamFilterDigest(b.Topics)
	if _, err = set.Replace(context.Background(), sharedauth.StreamPrincipal{}, b, nil); err == nil {
		t.Fatal("wildcard admitted")
	}
	if topics := set.Topics(); len(topics) != 1 || topics[0] != public {
		t.Fatal("malformed update changed prior grant", topics)
	}
	accepted[0] = private
	if set.Topics()[0] != public {
		t.Fatal("caller mutated granted scope")
	}
}

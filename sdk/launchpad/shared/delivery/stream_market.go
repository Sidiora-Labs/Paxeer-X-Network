package delivery

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
)

var marketPricePool = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var marketHeadTopic = regexp.MustCompile(`^market\.(stats|price)\.0x[0-9a-f]{40}$`)

// MarketProjectionHeadTx returns the current committed frame of a
// market.stats.<pool> or market.price.<pool> topic (nil when the topic has no
// frame yet) through api_stream.market_projection_head, so producers can derive
// the next revision without SELECT rights on the stream projection.
func (s StreamWriter) MarketProjectionHeadTx(ctx context.Context, tx pgx.Tx, topic string) ([]byte, error) {
	if ctx == nil || tx == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(s.Schema) || !marketHeadTopic.MatchString(topic) {
		return nil, errors.New("stream: market projection head requires a transaction and admitted topic")
	}
	var frame []byte
	if err := tx.QueryRow(ctx, "SELECT "+pgx.Identifier{s.Schema, "market_projection_head"}.Sanitize()+"($1)", topic).Scan(&frame); err != nil {
		return nil, err
	}
	return frame, nil
}

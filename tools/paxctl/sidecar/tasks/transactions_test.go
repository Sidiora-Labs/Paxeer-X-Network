package tasks

import (
	"errors"
	"fmt"
	"testing"

	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"
)

func TestIsTxNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "server-side tx not found",
			err:  &rpctypes.RPCError{Code: int(rpctypes.CodeInternalError), Message: "Internal error", Data: "tx (DEADBEEF) not found, err: index not enabled"},
			want: true,
		},
		{
			name: "server-side other internal error",
			err:  &rpctypes.RPCError{Code: int(rpctypes.CodeInternalError), Message: "Internal error", Data: "kvEventSink disabled"},
			want: false,
		},
		{
			name: "wrapped RPCError still discriminates",
			err:  fmt.Errorf("query /tx: %w", &rpctypes.RPCError{Code: int(rpctypes.CodeInternalError), Data: "tx (BEEF) not found, err: x"}),
			want: true,
		},
		{
			name: "transport error containing 'not found' is not classified as tx-not-found",
			err:  errors.New("Get http://seid:26657/tx: dial tcp: lookup seid: no such host (not found)"),
			want: false,
		},
		{
			name: "nil err",
			err:  nil,
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTxNotFound(c.err); got != c.want {
				t.Fatalf("isTxNotFound(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

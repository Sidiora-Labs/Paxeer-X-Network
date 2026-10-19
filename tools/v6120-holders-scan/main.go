// Command v6120-holders-scan completes the launchpad v6.12.0 holders.json
// fixture. It reads every pool token's totalSupply and the balances of a list
// of candidate addresses with eth_call at one height, keeps the nonzero
// balances, merges them into the existing holder lists and writes the result
// only when every token's holder balances sum to its totalSupply. It always
// prints a shortfall table to stderr and exits 1 while any token is short.
//
// Run from the repository root:
//
//	go run ./tools/v6120-holders-scan -candidates addresses.txt
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	chunkSize = 500
	workers   = 8
	callGas   = 10_000_000
)

// readerCode is installed with a state override at readerAddr. Its calldata is
// the token address word followed by one word per holder; it staticcalls
// token.balanceOf for each holder, reverts on any failed or short return and
// returns the balances in calldata order.
var (
	readerCode = hexutil.MustDecode("0x60205b80361115603d576370a082316000528035602052602060406024601c6000355afa" +
		"1560465760203d1060465760405181606001526020016002565b50602036036080f35b600080fd")
	readerAddr = common.HexToAddress("0x000000000000000000000000000000000000cA11")
	selTotal   = hexutil.MustDecode("0x18160ddd")
	selBalance = hexutil.MustDecode("0x70a08231")
)

type tokenHolders struct {
	Token   string   `json:"token"`
	Holders []string `json:"holders"`
}

type scanner struct {
	client *rpc.Client
	block  string
}

func (s *scanner) call(ctx context.Context, to common.Address, data []byte, override bool) ([]byte, error) {
	msg := map[string]interface{}{"to": to, "data": hexutil.Bytes(data), "gas": hexutil.Uint64(callGas)}
	args := []interface{}{msg, s.block}
	if override {
		args = append(args, map[common.Address]map[string]hexutil.Bytes{readerAddr: {"code": readerCode}})
	}
	var out hexutil.Bytes
	if err := s.client.CallContext(ctx, &out, "eth_call", args...); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *scanner) word(ctx context.Context, to common.Address, data []byte) (*big.Int, error) {
	out, err := s.call(ctx, to, data, false)
	if err != nil {
		return nil, err
	}
	if len(out) != 32 {
		return nil, fmt.Errorf("eth_call %s returned %d bytes", to.Hex(), len(out))
	}
	return new(big.Int).SetBytes(out), nil
}

func (s *scanner) balances(ctx context.Context, token common.Address, addrs []common.Address) ([]*big.Int, error) {
	data := make([]byte, 0, 32*(len(addrs)+1))
	data = append(data, common.LeftPadBytes(token.Bytes(), 32)...)
	for _, a := range addrs {
		data = append(data, common.LeftPadBytes(a.Bytes(), 32)...)
	}
	out, err := s.call(ctx, readerAddr, data, true)
	if err != nil {
		return nil, fmt.Errorf("balances of %s: %w", token.Hex(), err)
	}
	if len(out) != 32*len(addrs) {
		return nil, fmt.Errorf("balances of %s: %d bytes for %d holders", token.Hex(), len(out), len(addrs))
	}
	res := make([]*big.Int, len(addrs))
	for i := range addrs {
		res[i] = new(big.Int).SetBytes(out[32*i : 32*i+32])
	}
	return res, nil
}

// scan returns the balance of every address, reading chunks in parallel.
func (s *scanner) scan(ctx context.Context, token common.Address, addrs []common.Address) ([]*big.Int, error) {
	res := make([]*big.Int, len(addrs))
	starts := make(chan int)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for start := range starts {
				end := min(start+chunkSize, len(addrs))
				bals, err := s.balances(ctx, token, addrs[start:end])
				if err != nil {
					errs <- err
					return
				}
				copy(res[start:end], bals)
			}
		}()
	}
	var err error
feed:
	for start := 0; start < len(addrs); start += chunkSize {
		select {
		case starts <- start:
		case err = <-errs:
			break feed
		}
	}
	close(starts)
	wg.Wait()
	close(errs)
	if err != nil {
		return nil, err
	}
	if err = <-errs; err != nil {
		return nil, err
	}
	return res, nil
}

func readCandidates(path string) ([]common.Address, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw []string
	if t := bytes.TrimSpace(bz); len(t) > 0 && t[0] == '[' {
		if err := json.Unmarshal(t, &raw); err != nil {
			return nil, err
		}
	} else {
		raw = strings.Split(string(bz), "\n")
	}
	seen := map[common.Address]bool{}
	var out []common.Address
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if !common.IsHexAddress(r) || !strings.HasPrefix(r, "0x") {
			continue
		}
		a := common.HexToAddress(r)
		if a != (common.Address{}) && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out, nil
}

func run() error {
	rpcURL := flag.String("rpc", "http://127.0.0.1:18645", "archive JSON-RPC")
	height := flag.Uint64("height", 34742400, "block height to read balances at")
	existingPath := flag.String("existing", filepath.Join("modules", "launchpad", "keeper", "testdata", "v6120",
		"holders.json"), "existing holders.json")
	tokensPath := flag.String("tokens", "", "holders.json whose token set is scanned (default -existing)")
	candidatesPath := flag.String("candidates", "", "candidate addresses, one 0x address per line or a JSON list")
	outPath := flag.String("out", "holders.json", "merged holders.json, written only when every token balances")
	flag.Parse()
	if *candidatesPath == "" {
		return fmt.Errorf("-candidates is required")
	}
	if *tokensPath == "" {
		*tokensPath = *existingPath
	}

	var existing, tokenSet []tokenHolders
	for _, f := range []struct {
		path string
		into *[]tokenHolders
	}{{*existingPath, &existing}, {*tokensPath, &tokenSet}} {
		bz, err := os.ReadFile(f.path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(bz, f.into); err != nil {
			return fmt.Errorf("%s: %w", f.path, err)
		}
	}
	byToken := map[common.Address]int{}
	for i, t := range existing {
		byToken[common.HexToAddress(t.Token)] = i
	}
	for _, t := range tokenSet {
		a := common.HexToAddress(t.Token)
		if _, ok := byToken[a]; !ok {
			byToken[a] = len(existing)
			existing = append(existing, tokenHolders{Token: a.Hex(), Holders: []string{}})
		}
	}
	candidates, err := readCandidates(*candidatesPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d tokens, %d candidates, height %d\n", len(tokenSet), len(candidates), *height)

	client, err := rpc.Dial(*rpcURL)
	if err != nil {
		return err
	}
	defer client.Close()
	s := &scanner{client: client, block: hexutil.EncodeUint64(*height)}
	ctx := context.Background()

	tw := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "token\ttotalSupply\tsum\tshortfall\tholders found\t")
	short := 0
	for _, t := range tokenSet {
		token := common.HexToAddress(t.Token)
		entry := &existing[byToken[token]]
		total, err := s.word(ctx, token, selTotal)
		if err != nil {
			return fmt.Errorf("totalSupply of %s: %w", token.Hex(), err)
		}
		known := map[common.Address]bool{}
		var held []common.Address
		for _, h := range entry.Holders {
			a := common.HexToAddress(h)
			if !known[a] {
				known[a] = true
				held = append(held, a)
			}
		}
		sum := new(big.Int)
		if len(held) > 0 {
			bals, err := s.scan(ctx, token, held)
			if err != nil {
				return err
			}
			// The reader must agree with a direct balanceOf call.
			direct, err := s.word(ctx, token, append(append([]byte{}, selBalance...),
				common.LeftPadBytes(held[0].Bytes(), 32)...))
			if err != nil {
				return fmt.Errorf("balanceOf on %s: %w", token.Hex(), err)
			}
			if direct.Cmp(bals[0]) != 0 {
				return fmt.Errorf("reader returned %s for %s on %s, balanceOf returned %s", bals[0], held[0].Hex(),
					token.Hex(), direct)
			}
			for _, b := range bals {
				sum.Add(sum, b)
			}
		}
		found := 0
		if sum.Cmp(total) != 0 {
			bals, err := s.scan(ctx, token, candidates)
			if err != nil {
				return err
			}
			for i, a := range candidates {
				if bals[i].Sign() > 0 && !known[a] {
					known[a] = true
					entry.Holders = append(entry.Holders, a.Hex())
					sum.Add(sum, bals[i])
					found++
				}
			}
		}
		if sum.Cmp(total) != 0 {
			short++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t\n", token.Hex(), total, sum, new(big.Int).Sub(total, sum), found)
	}
	tw.Flush()
	if short > 0 {
		return fmt.Errorf("%d of %d tokens still short; %s not written", short, len(tokenSet), *outPath)
	}
	bz, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, append(bz, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "every token balances; wrote %s\n", *outPath)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

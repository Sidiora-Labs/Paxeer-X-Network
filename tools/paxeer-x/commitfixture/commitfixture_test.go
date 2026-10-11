package commitfixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/ed25519"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/coretypes"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/version"
)

const (
	chainID        = "hyperpax_125-1"
	height         = int64(1_000)
	round          = int32(0)
	validatorCount = 4
	votingPower    = int64(10)
	fixturePath    = "../../../explorer/backend/apps/explorer/test/support/fixture/paxeer_x/comet_commit.json"
)

type entry struct {
	Commit     *coretypes.ResultCommit     `json:"commit"`
	Validators *coretypes.ResultValidators `json:"validators"`
}

type fixture struct {
	ChainID    string `json:"chain_id"`
	FourOfFour entry  `json:"four_of_four"`
	TwoOfFour  entry  `json:"two_of_four"`
}

func filled(b byte) []byte {
	return bytes.Repeat([]byte{b}, 32)
}

func TestCometCommitFixture(t *testing.T) {
	ctx := context.Background()

	privByAddress := make(map[string]types.MockPV, validatorCount)
	validators := make([]*types.Validator, 0, validatorCount)
	for i := 0; i < validatorCount; i++ {
		pv := types.NewMockPVWithParams(ed25519.TestSecretKey([]byte(fmt.Sprintf("commitfixture-validator-%d", i))), false, false)
		pub, err := pv.GetPubKey(ctx)
		if err != nil {
			t.Fatalf("validator %d public key: %v", i, err)
		}
		privByAddress[string(pub.Address())] = pv
		validators = append(validators, types.NewValidator(pub, votingPower))
	}
	valSet := types.NewValidatorSet(validators)
	if valSet.Size() != validatorCount || valSet.TotalVotingPower() != validatorCount*votingPower {
		t.Fatalf("validator set size %d power %d", valSet.Size(), valSet.TotalVotingPower())
	}

	signers := make([]types.PrivValidator, 0, validatorCount)
	for _, val := range valSet.Validators {
		pv, ok := privByAddress[string(val.Address)]
		if !ok {
			t.Fatalf("no private validator for %X", val.Address)
		}
		signers = append(signers, pv)
	}

	header := &types.Header{
		Version: version.Consensus{Block: version.BlockProtocol, App: 1},
		ChainID: chainID,
		Height:  height,
		Time:    time.Unix(1_700_000_000, 123_456_789).UTC(),
		LastBlockID: types.BlockID{
			Hash:          filled(0x01),
			PartSetHeader: types.PartSetHeader{Total: 3, Hash: filled(0x02)},
		},
		LastCommitHash:     filled(0x03),
		DataHash:           filled(0x04),
		ValidatorsHash:     valSet.Hash(),
		NextValidatorsHash: valSet.Hash(),
		ConsensusHash:      filled(0x05),
		AppHash:            filled(0x06),
		LastResultsHash:    filled(0x07),
		EvidenceHash:       filled(0x08),
		ProposerAddress:    valSet.Validators[0].Address,
	}
	blockID := types.BlockID{
		Hash:          header.Hash(),
		PartSetHeader: types.PartSetHeader{Total: 1, Hash: filled(0x09)},
	}

	voteSet := types.NewVoteSet(chainID, height, round, tmproto.PrecommitType, valSet)
	commit, err := types.MakeCommit(ctx, blockID, height, round, voteSet, signers, header.Time.Add(1_250*time.Millisecond))
	if err != nil {
		t.Fatalf("MakeCommit: %v", err)
	}
	if err := types.VerifyCommitLight(chainID, valSet, blockID, height, commit); err != nil {
		t.Fatalf("VerifyCommitLight four of four: %v", err)
	}
	if !bytes.Equal(commit.BlockID.Hash, header.Hash()) {
		t.Fatalf("commit block hash %X differs from header hash %X", commit.BlockID.Hash, header.Hash())
	}

	partial := &types.Commit{
		Height:     commit.Height,
		Round:      commit.Round,
		BlockID:    commit.BlockID,
		Signatures: append([]types.CommitSig(nil), commit.Signatures...),
	}
	for i := 2; i < validatorCount; i++ {
		partial.Signatures[i] = types.NewCommitSigAbsent()
	}
	err = types.VerifyCommitLight(chainID, valSet, blockID, height, partial)
	var short types.ErrNotEnoughVotingPowerSigned
	if !types.IsErrNotEnoughVotingPowerSigned(err) {
		t.Fatalf("VerifyCommitLight two of four: got %v, want insufficient voting power", err)
	}
	if !errors.As(err, &short) || short.Got != 2*votingPower || short.Needed != validatorCount*votingPower*2/3 {
		t.Fatalf("VerifyCommitLight two of four: %v", err)
	}

	result := func(c *types.Commit) entry {
		return entry{
			Commit: coretypes.NewResultCommit(header, c, true),
			Validators: &coretypes.ResultValidators{
				BlockHeight: height,
				Validators:  valSet.Validators,
				Count:       valSet.Size(),
				Total:       valSet.Size(),
			},
		}
	}
	want, err := json.MarshalIndent(fixture{
		ChainID:    chainID,
		FourOfFour: result(commit),
		TwoOfFour:  result(partial),
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	want = append(want, '\n')

	path := filepath.FromSlash(fixturePath)
	if os.Getenv("PAXEER_X_WRITE_FIXTURE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("fixture directory: %v", err)
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the commit this test builds; rerun with PAXEER_X_WRITE_FIXTURE=1", fixturePath)
	}
}

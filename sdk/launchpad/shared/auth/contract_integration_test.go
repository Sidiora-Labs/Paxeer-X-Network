package auth

import (
	"context"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

// Real source-backed ERC1271 fixture verifies ECDSA signer and revocation.
func TestRealEVMContractMagicAndRevocation(t *testing.T) {
	endpoint := os.Getenv("L34_ANVIL_URL")
	if endpoint == "" {
		t.Skip("requires lease-owned Anvil")
	}
	ctx := context.Background()
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	chain, err := client.ChainID(ctx)
	if err != nil || chain.Cmp(big.NewInt(125)) != 0 {
		t.Fatal("private fixture chain", err)
	}
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	if err = client.Client().CallContext(ctx, nil, "anvil_setBalance", sender, "0x56bc75e2d63100000"); err != nil {
		t.Fatal(err)
	}
	bytecode, err := os.ReadFile("testdata/TestWallet.bin")
	if err != nil {
		t.Fatal(err)
	}
	init := append(common.FromHex(string(bytecode)), common.LeftPadBytes(sender.Bytes(), 32)...)
	price, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx := types.NewContractCreation(0, big.NewInt(0), 500000, price, init)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(125)), key)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SendTransaction(ctx, signed); err != nil {
		t.Fatal(err)
	}
	receipt, err := bind.WaitMined(ctx, client, signed)
	if err != nil || receipt.Status != 1 {
		t.Fatal("deployment", err)
	}
	wallet := receipt.ContractAddress.Hex()
	walletSig, _ := crypto.Sign(accounts.TextHash([]byte("exact")), key)
	proof, err := VerifyContract(ctx, NewReportedHashBackend(client.Client()), wallet, "exact", hexutil.Encode(walletSig), nil)
	if err != nil || proof.ChainID != 125 || proof.BlockHash == "" || proof.CodeHash == "" {
		t.Fatal(proof, err)
	}
	var observations []string
	observe := func(method, category string, duration time.Duration) {
		if duration <= 0 {
			t.Fatal("verification duration missing")
		}
		observations = append(observations, method+":"+category)
	}
	if _, err = VerifyProof(ctx, NewReportedHashBackend(client.Client()), WalletProof{Kind: "eoa_eip191", Wallet: sender.Hex(), Signature: hexutil.Encode(walletSig)}, "exact", observe); err != nil {
		t.Fatal("EOA proof denied", err)
	}
	if len(observations) != 1 || observations[0] != "eip191:accepted" {
		t.Fatal("EOA success observation", observations)
	}
	observations = nil
	if _, err = VerifyProof(ctx, NewReportedHashBackend(client.Client()), WalletProof{Kind: "eoa_eip191", Wallet: wallet, Signature: goldenSig}, goldenMsg, observe); err == nil {
		t.Fatal("EOA kind bypass")
	}
	if len(observations) != 1 || observations[0] != "eip191:proof_kind" {
		t.Fatal("EOA code rejection observation", observations)
	}
	if _, err = VerifyContract(ctx, NewReportedHashBackend(client.Client()), wallet, "changed", hexutil.Encode(walletSig), nil); err == nil {
		t.Fatal("wrong digest accepted")
	}
	revokeData := crypto.Keccak256([]byte("revoke()"))[:4]
	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		t.Fatal(err)
	}
	revokeTx := types.NewTransaction(nonce, receipt.ContractAddress, big.NewInt(0), 100000, price, revokeData)
	signedRevoke, err := types.SignTx(revokeTx, types.NewEIP155Signer(big.NewInt(125)), key)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SendTransaction(ctx, signedRevoke); err != nil {
		t.Fatal(err)
	}
	if _, err = bind.WaitMined(ctx, client, signedRevoke); err != nil {
		t.Fatal(err)
	}
	if err = client.Client().CallContext(ctx, nil, "evm_mine"); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyContract(ctx, NewReportedHashBackend(client.Client()), wallet, "exact", hexutil.Encode(walletSig), nil); err == nil {
		t.Fatal("revoked contract accepted")
	}
}

func TestPhase2CommittedContractRevocation(t *testing.T) {
	pool, _ := phase2AuthDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	endpoint := os.Getenv("L34_ANVIL_URL")
	if endpoint == "" {
		t.Fatal("owned chain125 required")
	}
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	if err = client.Client().CallContext(ctx, nil, "anvil_setBalance", sender, "0x56bc75e2d63100000"); err != nil {
		t.Fatal(err)
	}
	bytecode, err := os.ReadFile("testdata/TestWallet.bin")
	if err != nil {
		t.Fatal(err)
	}
	price, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deployment := types.NewContractCreation(0, big.NewInt(0), 500000, price, append(common.FromHex(string(bytecode)), common.LeftPadBytes(sender.Bytes(), 32)...))
	signed, err := types.SignTx(deployment, types.NewEIP155Signer(big.NewInt(125)), key)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SendTransaction(ctx, signed); err != nil {
		t.Fatal(err)
	}
	receipt, err := bind.WaitMined(ctx, client, signed)
	if err != nil || receipt.Status != 1 {
		t.Fatal("real wallet deploy", err)
	}
	block, err := NewReportedHashBackend(client.Client()).ReportedBlockByNumber(ctx, receipt.BlockNumber)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO auth_committed_blocks VALUES(125,$1,$2,'healthy')`, block.Header.Number.String(), block.Hash.Hex()); err != nil {
		t.Fatal(err)
	}
	var methods []string
	verifier := CommittedProofVerifier{Backend: NewReportedHashBackend(client.Client()), Blocks: PostgresCommittedBlockProvider{Pool: pool}, Observe: func(method, category string, duration time.Duration) {
		if duration <= 0 {
			t.Fatal("no proof timing")
		}
		methods = append(methods, method+":"+category)
	}}
	signature, err := crypto.Sign(accounts.TextHash([]byte("committed authorization")), key)
	if err != nil {
		t.Fatal(err)
	}
	wallet := strings.ToLower(receipt.ContractAddress.Hex())
	if err = verifier.Verify(ctx, wallet, "committed authorization", hexutil.Encode(signature)); err != nil {
		t.Fatal("valid committed contract", err)
	}
	if len(methods) != 1 || methods[0] != "eip1271:accepted" {
		t.Fatal(methods)
	}
	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		t.Fatal(err)
	}
	revoke := types.NewTransaction(nonce, receipt.ContractAddress, big.NewInt(0), 100000, price, crypto.Keccak256([]byte("revoke()"))[:4])
	signed, err = types.SignTx(revoke, types.NewEIP155Signer(big.NewInt(125)), key)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SendTransaction(ctx, signed); err != nil {
		t.Fatal(err)
	}
	revocation, err := bind.WaitMined(ctx, client, signed)
	if err != nil || revocation.Status != 1 {
		t.Fatal(err)
	}
	block, err = NewReportedHashBackend(client.Client()).ReportedBlockByNumber(ctx, revocation.BlockNumber)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO auth_committed_blocks VALUES(125,$1,$2,'healthy')`, block.Header.Number.String(), block.Hash.Hex()); err != nil {
		t.Fatal(err)
	}
	if err = verifier.Verify(ctx, wallet, "committed authorization", hexutil.Encode(signature)); err == nil {
		t.Fatal("revoked committed wallet accepted")
	}
	if _, err = pool.Exec(ctx, `UPDATE auth_committed_blocks SET integrity='quarantined' WHERE height=$1`, block.Header.Number.String()); err != nil {
		t.Fatal(err)
	}
	err = verifier.Verify(ctx, wallet, "committed authorization", hexutil.Encode(signature))
	status, _, retryable := PublicCode(err)
	if status != 503 || !retryable {
		t.Fatal("quarantine fell back", status, err)
	}
	t.Log("D11_005 committed_contract_accept=1 revoked_contract_reject=1 quarantined_watermark_503=1")
}

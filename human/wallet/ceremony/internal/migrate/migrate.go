package migrate

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"filippo.io/edwards25519"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	_ "github.com/lib/pq"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/dealer"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/attestor"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
)

const EnvDatabaseURL = "CEREMONY_DATABASE_URL"

var (
	ErrDatabaseURL     = errors.New("migrate: database connection string is not set")
	ErrSchema          = errors.New("migrate: wallets table lacks the migrated_at column")
	ErrAddressMismatch = errors.New("migrate: verification signature does not recover the stored address")
	ErrSplitMismatch   = errors.New("migrate: dealer public key does not derive the stored address")
	ErrIdentityKey     = errors.New("migrate: identity key split does not match its public key")
	ErrMarkMigrated    = errors.New("migrate: wallet row was not marked migrated")
	ErrIdentitySig     = errors.New("migrate: verification signature does not verify against the imported identity key")
)

const fundedPredicate = `(w.kind = 'funded' or exists (select 1 from funded_accounts f where f.wallet_id = w.id))`

func FundedPredicate() string { return fundedPredicate }

type Wallet struct {
	ID                  string
	UserID              string
	Address             string
	EncryptedPrivateKey string
	KeyVersion          int
	ChainID             int64
	Kind                string
	Funded              bool
	Migrated            bool
}

type Plan struct {
	Total           int
	Funded          []Wallet
	AlreadyMigrated int
	Eligible        []Wallet
}

type Report struct {
	Read       int
	Verified   int
	Imported   int
	Refreshed  int
	TestSigned int
	Matched    int
}

type MismatchError struct {
	WalletID  string
	Stored    common.Address
	Recovered common.Address
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("%v: wallet %s stored %s recovered %s", ErrAddressMismatch, e.WalletID, e.Stored.Hex(), e.Recovered.Hex())
}

func (e *MismatchError) Unwrap() error { return ErrAddressMismatch }

type WalletError struct {
	WalletID string
	Stage    string
	Err      error
}

func (e *WalletError) Error() string {
	return fmt.Sprintf("migrate: wallet %s at %s: %v", e.WalletID, e.Stage, e.Err)
}

func (e *WalletError) Unwrap() error { return e.Err }

func Open(getenv func(string) string) (*sql.DB, error) {
	dsn := strings.TrimSpace(getenv(EnvDatabaseURL))
	if dsn == "" {
		return nil, fmt.Errorf("%w: %s", ErrDatabaseURL, EnvDatabaseURL)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("migrate: open database: %w", err)
	}
	return db, nil
}

func checkSchema(ctx context.Context, db *sql.DB) error {
	var n int
	err := db.QueryRowContext(ctx, `select count(*) from information_schema.columns
		where table_schema = current_schema() and table_name = 'wallets' and column_name = 'migrated_at'`).Scan(&n)
	if err != nil {
		return fmt.Errorf("migrate: inspect schema: %w", err)
	}
	if n != 1 {
		return ErrSchema
	}
	return nil
}

func ReadWallets(ctx context.Context, db *sql.DB) ([]Wallet, error) {
	if err := checkSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `select w.id::text, w.user_id::text, w.address, w.encrypted_private_key,
			w.key_version, w.chain_id, w.kind, `+fundedPredicate+`, w.migrated_at is not null
		from wallets w order by w.created_at, w.id`)
	if err != nil {
		return nil, fmt.Errorf("migrate: read wallets: %w", err)
	}
	defer rows.Close()
	var out []Wallet
	for rows.Next() {
		var w Wallet
		if err := rows.Scan(&w.ID, &w.UserID, &w.Address, &w.EncryptedPrivateKey, &w.KeyVersion, &w.ChainID, &w.Kind, &w.Funded, &w.Migrated); err != nil {
			return nil, fmt.Errorf("migrate: scan wallet: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate: read wallets: %w", err)
	}
	return out, nil
}

func PlanMigration(ctx context.Context, db *sql.DB) (*Plan, error) {
	wallets, err := ReadWallets(ctx, db)
	if err != nil {
		return nil, err
	}
	p := &Plan{Total: len(wallets)}
	for _, w := range wallets {
		switch {
		case w.Funded:
			p.Funded = append(p.Funded, w)
		case w.Migrated:
			p.AlreadyMigrated++
		default:
			p.Eligible = append(p.Eligible, w)
		}
	}
	return p, nil
}

func SecpKeyID(walletID string) string { return "wallet:" + walletID + ":secp256k1" }

func IdentityKeyID(walletID string) string { return "wallet:" + walletID + ":ed25519" }

func Deliver(ctx context.Context, db *sql.DB, client *attestor.Client, masterKey []byte, plan *Plan) (Report, error) {
	var r Report
	if err := checkSchema(ctx, db); err != nil {
		return r, err
	}
	nodes := client.NodeIDs()
	for _, w := range plan.Eligible {
		r.Read++
		if err := deliverOne(ctx, db, client, masterKey, nodes, w, &r); err != nil {
			return r, err
		}
	}
	return r, nil
}

func deliverOne(ctx context.Context, db *sql.DB, client *attestor.Client, masterKey []byte, nodes []string, w Wallet, r *Report) error {
	wrap := func(stage string, err error) error { return &WalletError{WalletID: w.ID, Stage: stage, Err: err} }

	key, err := envelope.OpenEnvelope(w.EncryptedPrivateKey, masterKey, w.Address)
	if err != nil {
		return wrap("open", err)
	}
	stored := key.Address()
	secret, err := key.Scalar()
	key.Zero()
	if err != nil {
		return wrap("open", err)
	}
	r.Verified++

	secpBundles, secpPub, err := dealer.Split(dealer.Secp256k1, secret, nodes)
	if err != nil {
		return wrap("split secp256k1", err)
	}
	defer wipeBundles(secpBundles)
	if crypto.PubkeyToAddress(ecdsa.PublicKey{Curve: crypto.S256(), X: secpPub.GetX(), Y: secpPub.GetY()}) != stored {
		return wrap("split secp256k1", ErrSplitMismatch)
	}

	edBundles, edPub, err := splitIdentity(nodes)
	if err != nil {
		return wrap("split ed25519", err)
	}
	defer wipeBundles(edBundles)

	secpImport, err := client.Import(ctx, SecpKeyID(w.ID), w.UserID, stored.Hex(), secpBundles, secpPub)
	if err != nil {
		return wrap("import secp256k1", err)
	}
	wipeBundles(secpBundles)
	edImport, err := client.Import(ctx, IdentityKeyID(w.ID), w.UserID, stored.Hex(), edBundles, edPub)
	if err != nil {
		return wrap("import ed25519", err)
	}
	wipeBundles(edBundles)
	r.Imported++

	if _, err := client.Refresh(ctx, SecpKeyID(w.ID), secpPub); err != nil {
		return wrap("refresh secp256k1", err)
	}
	if _, err := client.Refresh(ctx, IdentityKeyID(w.ID), edPub); err != nil {
		return wrap("refresh ed25519", err)
	}
	r.Refreshed++

	secpSig, err := client.SignVerification(ctx, SecpKeyID(w.ID), secpImport.SessionID, secpPub)
	if err != nil {
		return wrap("verify secp256k1", err)
	}
	edSig, err := client.SignVerification(ctx, IdentityKeyID(w.ID), edImport.SessionID, edPub)
	if err != nil {
		return wrap("verify ed25519", err)
	}
	r.TestSigned++

	pub, err := crypto.SigToPub(crypto.Keccak256(secpSig.Message), secpSig.Signature)
	if err != nil {
		return wrap("recover", err)
	}
	recovered := crypto.PubkeyToAddress(*pub)
	if recovered != stored {
		return &MismatchError{WalletID: w.ID, Stored: stored, Recovered: recovered}
	}
	identity, err := attestor.PublicKeyBytes(edPub)
	if err != nil {
		return wrap("verify ed25519", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(identity), edSig.Message, edSig.Signature) {
		return wrap("verify ed25519", ErrIdentitySig)
	}

	res, err := db.ExecContext(ctx, `update wallets set migrated_at = now(), attestor_key_id = $3
		where id = $1::uuid and migrated_at is null and lower(address) = lower($2)`, w.ID, stored.Hex(), SecpKeyID(w.ID))
	if err != nil {
		return wrap("mark", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return wrap("mark", ErrMarkMigrated)
	}
	r.Matched++
	return nil
}

func splitIdentity(nodes []string) ([]dealer.ShareBundle, *pt.ECPoint, error) {
	seed := make([]byte, ed25519.SeedSize)
	defer zero(seed)
	if _, err := rand.Read(seed); err != nil {
		return nil, nil, err
	}
	digest := sha512.Sum512(seed)
	defer zero(digest[:])
	s, err := edwards25519.NewScalar().SetBytesWithClamping(digest[:32])
	if err != nil {
		return nil, nil, err
	}
	le := s.Bytes()
	defer zero(le)
	be := make([]byte, len(le))
	defer zero(be)
	for i := range le {
		be[len(le)-1-i] = le[i]
	}
	secret := new(big.Int).SetBytes(be)
	priv := ed25519.NewKeyFromSeed(seed)
	defer zero(priv)
	want := priv.Public().(ed25519.PublicKey)
	bundles, pub, err := dealer.Split(dealer.Ed25519, secret, nodes)
	if err != nil {
		return nil, nil, err
	}
	enc, err := attestor.EncodePoint(pub)
	if err != nil || enc != fmt.Sprintf("%x", []byte(want)) {
		wipeBundles(bundles)
		return nil, nil, ErrIdentityKey
	}
	return bundles, pub, nil
}

func wipeBundles(bs []dealer.ShareBundle) {
	for _, b := range bs {
		if b.Share != nil {
			words := b.Share.Bits()
			for i := range words {
				words[i] = 0
			}
			b.Share.SetInt64(0)
		}
	}
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

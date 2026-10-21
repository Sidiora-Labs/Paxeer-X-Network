package migrate

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"
	"time"

	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	_ "github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/dealer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/attestor"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/envelope"
)

const EnvDatabaseURL = "CEREMONY_DATABASE_URL"

var (
	ErrDatabaseURL      = errors.New("migrate: database connection string is not set")
	ErrSchema           = errors.New("migrate: wallets table lacks the migrated_at column")
	ErrAddressMismatch  = errors.New("migrate: verification signature does not recover the stored address")
	ErrSplitMismatch    = errors.New("migrate: dealer public key does not derive the stored address")
	ErrIdentityKey      = errors.New("migrate: identity key split does not match its public key")
	ErrMarkMigrated     = errors.New("migrate: wallet row was not marked migrated")
	ErrIdentityRecovery = errors.New("migrate: original identity recovery authority is unavailable; replacement identities are forbidden")
	ErrExternalIdentity = errors.New("migrate: external agent identity remains externally held; required two-curve custody migration is not authorized")
	ErrJournal          = errors.New("migrate: durable ceremony journal is missing, invalid or conflicts with original identity")
	ErrRehearsal        = errors.New("migrate: a complete matching rehearsal receipt is required before live migration")
	ErrIdentitySig      = errors.New("migrate: verification signature does not verify against the imported identity key")
)

const fundedPredicate = `(w.kind = 'funded' or exists (select 1 from funded_accounts f where f.wallet_id = w.id))`

func FundedPredicate() string { return fundedPredicate }

type Wallet struct {
	ID                    string
	UserID                string
	Address               string
	EncryptedPrivateKey   string
	KeyVersion            int
	ChainID               int64
	Kind                  string
	Funded                bool
	Migrated              bool
	DID                   string
	MainAccountID         string
	OriginalIdentityKeyID string
}

type Plan struct {
	Total           int
	Funded          []Wallet
	AlreadyMigrated int
	Eligible        []Wallet
}

type Report struct {
	Read             int
	Verified         int
	Imported         int
	Refreshed        int
	TestSigned       int
	HeldIdentity     int
	ExternalIdentity int
	Matched          int
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
	rows, err := db.QueryContext(ctx, `select w.id::text, w.user_id::text, w.address, coalesce(w.encrypted_private_key, ''),
			w.key_version, w.chain_id, w.kind, `+fundedPredicate+`, w.migrated_at is not null, coalesce(w.did, ''), coalesce(w.main_account_id, ''), coalesce(w.layerx_key_id, '')
		from wallets w order by w.created_at, w.id`)
	if err != nil {
		return nil, fmt.Errorf("migrate: read wallets: %w", err)
	}
	defer rows.Close()
	var out []Wallet
	for rows.Next() {
		var w Wallet
		if err := rows.Scan(&w.ID, &w.UserID, &w.Address, &w.EncryptedPrivateKey, &w.KeyVersion, &w.ChainID, &w.Kind, &w.Funded, &w.Migrated, &w.DID, &w.MainAccountID, &w.OriginalIdentityKeyID); err != nil {
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

type Options struct {
	CeremonyID       string
	JournalDir       string
	JournalKey       []byte
	RehearsalReceipt string
	Rehearsal        bool
	RPCURL           string
}

type curveJournal struct {
	KeyID                  string
	Curve                  string
	PublicKey              string
	ImportSession          string
	RefreshSession         string
	VerifySession          string
	RecoverySession        string
	RefreshRecoverySession string
	EpochCaptured          bool
	BaseEpoch              uint64
	Shares                 []attestor.ShareBundleJSON
	Imported               bool
	Refreshed              bool
	Epoch                  uint64
	PossessionMessage      []byte
	Ownership              *attestor.Verification
	Verification           *attestor.Verification
}

type walletJournal struct {
	Version      int
	CeremonyID   string
	WalletDigest string
	Members      []attestor.NodeConfig
	Secp         curveJournal
	Identity     curveJournal
	Original     originalIdentity
	Matched      bool
}

type sealed struct {
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

type rehearsalReceipt struct {
	Version    int
	CeremonyID string
	CreatedAt  int64
	Members    []attestor.NodeConfig
	Wallets    map[string]string
	Counts     Report
}

func protectedRead(path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrJournal
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || st.Size() > limit {
		return nil, ErrJournal
	}
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func LoadOptions(getenv func(string) string) (Options, error) {
	o := Options{CeremonyID: strings.TrimSpace(getenv("CEREMONY_ID")), JournalDir: strings.TrimSpace(getenv("CEREMONY_JOURNAL_DIR")), RehearsalReceipt: strings.TrimSpace(getenv("CEREMONY_REHEARSAL_RECEIPT")), RPCURL: strings.TrimSpace(getenv("CEREMONY_RPC_URL"))}
	var err error
	o.JournalKey, err = protectedRead(strings.TrimSpace(getenv("CEREMONY_JOURNAL_KEY_FILE")), 32)
	if err != nil || len(o.JournalKey) != 32 {
		zero(o.JournalKey)
		return Options{}, ErrJournal
	}
	if err = o.validate(); err != nil {
		zero(o.JournalKey)
		return Options{}, err
	}
	return o, nil
}

func (o Options) validate() error {
	if o.CeremonyID == "" || len(o.CeremonyID) > 128 || strings.ContainsAny(o.CeremonyID, "/\\") || len(o.JournalKey) != 32 || !filepath.IsAbs(o.JournalDir) || !filepath.IsAbs(o.RehearsalReceipt) {
		return ErrJournal
	}
	st, err := os.Lstat(o.JournalDir)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return ErrJournal
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return ErrJournal
	}
	return nil
}

func walletDigest(w Wallet) string {
	raw, _ := json.Marshal(struct {
		ID, UserID, Address, Envelope, Kind, DID, Main string
		Version                                        int
		Chain                                          int64
		Funded                                         bool
	}{w.ID, w.UserID, strings.ToLower(w.Address), w.EncryptedPrivateKey, w.Kind, w.DID, w.MainAccountID + ":" + w.OriginalIdentityKeyID, w.KeyVersion, w.ChainID, w.Funded})
	defer zero(raw)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func sealValue(key []byte, domain string, value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	defer zero(raw)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return json.Marshal(sealed{Nonce: nonce, Ciphertext: gcm.Seal(nil, nonce, raw, []byte(domain))})
}

func openValue(key []byte, domain string, raw []byte, value any) error {
	var box sealed
	if err := strictJSON(raw, &box); err != nil {
		return ErrJournal
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ErrJournal
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(box.Nonce) != gcm.NonceSize() {
		return ErrJournal
	}
	plain, err := gcm.Open(nil, box.Nonce, box.Ciphertext, []byte(domain))
	if err != nil {
		return ErrJournal
	}
	defer zero(plain)
	if err = strictJSON(plain, value); err != nil {
		return ErrJournal
	}
	return nil
}

func strictJSON(raw []byte, value any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return ErrJournal
	}
	return nil
}

func atomicSealed(path string, key []byte, domain string, value any) error {
	raw, err := sealValue(key, domain, value)
	if err != nil {
		return err
	}
	defer zero(raw)
	dir := filepath.Dir(path)
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return ErrJournal
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return ErrJournal
	}
	f, err := os.CreateTemp(dir, ".ceremony-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func journalDomain(o Options, id string) string {
	return "PXW:CEREMONY-JOURNAL:v1:" + o.CeremonyID + ":" + id
}
func journalPath(o Options, id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(o.JournalDir, hex.EncodeToString(sum[:])+".sealed")
}

func WriteRehearsalReceipt(o Options, plan *Plan, client *attestor.Client, report Report) error {
	if !o.Rehearsal || plan == nil || o.validate() != nil {
		return ErrRehearsal
	}
	n := len(plan.Eligible)
	if !completeCounts(report, n) {
		return ErrRehearsal
	}
	receipt := rehearsalReceipt{Version: 1, CeremonyID: o.CeremonyID, CreatedAt: time.Now().Unix(), Members: client.Membership(), Wallets: map[string]string{}, Counts: report}
	for _, w := range plan.Eligible {
		receipt.Wallets[w.ID] = walletDigest(w)
	}
	return atomicSealed(o.RehearsalReceipt, o.JournalKey, "PXW:CEREMONY-REHEARSAL:v1:"+o.CeremonyID, receipt)
}

func requireRehearsal(o Options, plan *Plan, client *attestor.Client) error {
	raw, err := protectedRead(o.RehearsalReceipt, 4<<20)
	if err != nil {
		return ErrRehearsal
	}
	defer zero(raw)
	var receipt rehearsalReceipt
	if openValue(o.JournalKey, "PXW:CEREMONY-REHEARSAL:v1:"+o.CeremonyID, raw, &receipt) != nil || receipt.Version != 1 || receipt.CeremonyID != o.CeremonyID || !reflect.DeepEqual(receipt.Members, client.Membership()) || receipt.CreatedAt <= 0 || receipt.CreatedAt > time.Now().Unix() {
		return ErrRehearsal
	}
	n := len(receipt.Wallets)
	if !completeCounts(receipt.Counts, n) {
		return ErrRehearsal
	}
	for _, w := range plan.Eligible {
		if receipt.Wallets[w.ID] != walletDigest(w) {
			return ErrRehearsal
		}
	}
	return nil
}

func Deliver(ctx context.Context, db *sql.DB, client *attestor.Client, masterKey []byte, plan *Plan, options ...Options) (Report, error) {
	var r Report
	if err := checkSchema(ctx, db); err != nil {
		return r, err
	}
	if plan == nil || client == nil {
		return r, ErrJournal
	}
	var o Options
	var err error
	if len(options) == 0 {
		o, err = LoadOptions(os.Getenv)
		if err != nil {
			return r, err
		}
		defer zero(o.JournalKey)
	} else if len(options) == 1 {
		o = options[0]
	} else {
		return r, ErrJournal
	}
	if err = o.validate(); err != nil {
		return r, err
	}
	if !o.Rehearsal {
		if err = requireRehearsal(o, plan, client); err != nil {
			return r, err
		}
	}
	if len(client.NodeIDs()) != 5 || attestor.SignQuorum != 3 {
		return r, attestor.ErrConfig
	}
	for _, w := range plan.Eligible {
		r.Read++
		if err = deliverOne(ctx, db, client, masterKey, w, &r, o); err != nil {
			return r, err
		}
	}
	return r, nil
}

func deliverOne(ctx context.Context, db *sql.DB, client *attestor.Client, masterKey []byte, w Wallet, r *Report, o Options) error {
	wrap := func(stage string, err error) error { return &WalletError{WalletID: w.ID, Stage: stage, Err: err} }
	connection, err := db.Conn(ctx)
	if err != nil {
		return wrap("wallet lock", err)
	}
	defer connection.Close()
	var locked bool
	if err = connection.QueryRowContext(ctx, `select pg_try_advisory_lock(hashtextextended($1,0))`, "PXW:CEREMONY:"+w.ID).Scan(&locked); err != nil || !locked {
		return wrap("wallet lock", ErrJournal)
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = connection.ExecContext(release, `select pg_advisory_unlock(hashtextextended($1,0))`, "PXW:CEREMONY:"+w.ID)
	}()
	lockPath := journalPath(o, w.ID) + ".lock"
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return wrap("journal", err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	defer lock.Close()
	info, e := lock.Stat()
	if e != nil {
		return wrap("journal lock", e)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		return wrap("journal lock", ErrJournal)
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return wrap("journal locked", err)
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	path := journalPath(o, w.ID)
	original, err := loadOriginalIdentity(ctx, db, w)
	if err != nil {
		return wrap("original identity", err)
	}
	var currentNonce uint64
	if err = verifyOriginalBinding(ctx, o.RPCURL, w, original, &currentNonce); err != nil {
		return wrap("original binding", err)
	}
	if original.External {
		return wrap("external identity custody", ErrExternalIdentity)
	}
	var journal walletJournal
	raw, err := protectedRead(path, 16<<20)
	if err == nil {
		err = openValue(o.JournalKey, journalDomain(o, w.ID), raw, &journal)
		zero(raw)
		if err != nil {
			return wrap("journal", err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if e := client.RequireAbsent(ctx, SecpKeyID(w.ID)); e != nil {
			return wrap("missing journal for existing key", e)
		}
		key, e := envelope.OpenEnvelope(w.EncryptedPrivateKey, masterKey, w.Address)
		if e != nil {
			return wrap("open", e)
		}
		scalar, e := key.Scalar()
		key.Zero()
		if e != nil {
			return wrap("open", e)
		}
		defer wipeInt(scalar)
		secpBundles, secpPub, e := dealer.Split(dealer.Secp256k1, scalar, client.NodeIDs())
		if e != nil {
			return wrap("split secp256k1", e)
		}
		defer wipeBundles(secpBundles)
		if crypto.PubkeyToAddress(ecdsa.PublicKey{Curve: crypto.S256(), X: secpPub.GetX(), Y: secpPub.GetY()}) != common.HexToAddress(w.Address) {
			return wrap("split secp256k1", ErrSplitMismatch)
		}
		journal = walletJournal{Version: 1, CeremonyID: o.CeremonyID, WalletDigest: walletDigest(w), Members: client.Membership(), Original: original}
		journal.Secp, e = newCurveJournal(SecpKeyID(w.ID), secpBundles, secpPub)
		if e != nil {
			return wrap("journal", e)
		}
		possession := original
		possession.BindNonce = currentNonce
		journal.Identity = curveJournal{KeyID: original.KeyID, Curve: attestor.CurveEd25519, PublicKey: original.PublicKey, PossessionMessage: possession.message(w)}
		for _, out := range []*string{&journal.Identity.RefreshSession, &journal.Identity.VerifySession} {
			*out, e = attestor.NewSessionID()
			if e != nil {
				return wrap("journal", e)
			}
		}
		if e = atomicSealed(path, o.JournalKey, journalDomain(o, w.ID), journal); e != nil {
			return wrap("journal", e)
		}
	} else {
		return wrap("journal", err)
	}
	defer clearJournal(&journal)
	if journal.Version != 1 || journal.CeremonyID != o.CeremonyID || journal.WalletDigest != walletDigest(w) || !reflect.DeepEqual(journal.Members, client.Membership()) {
		return wrap("journal conflict", ErrJournal)
	}
	if journal.Secp.KeyID != SecpKeyID(w.ID) || journal.Identity.KeyID != original.KeyID || !reflect.DeepEqual(journal.Original, original) {
		return wrap("journal identity", ErrJournal)
	}
	r.Verified++
	save := func() error { return atomicSealed(path, o.JournalKey, journalDomain(o, w.ID), journal) }
	if err = resumeCurve(ctx, client, o, w, &journal.Secp, save); err != nil {
		return wrap("resume secp256k1", err)
	}
	if err = resumeHeldIdentity(ctx, client, o, w, &journal.Identity, original, save); err != nil {
		return wrap("held original identity", err)
	}
	r.HeldIdentity++
	r.Imported++
	r.Refreshed++
	r.TestSigned++
	secp := journal.Secp.Verification
	ed := journal.Identity.Verification
	if secp == nil || ed == nil {
		return wrap("verification evidence", ErrJournal)
	}
	pub, err := crypto.SigToPub(crypto.Keccak256(secp.Message), secp.Signature)
	if err != nil {
		return wrap("recover", err)
	}
	recovered := crypto.PubkeyToAddress(*pub)
	stored := common.HexToAddress(w.Address)
	if recovered != stored {
		return &MismatchError{WalletID: w.ID, Stored: stored, Recovered: recovered}
	}
	identity, err := hex.DecodeString(strings.TrimPrefix(w.DID, "did:layerx:"))
	if err != nil || len(identity) != 32 || !ed25519.Verify(identity, ed.Message, ed.Signature) {
		return wrap("verify original identity", ErrIdentitySig)
	}
	journal.Matched = true
	if err = save(); err != nil {
		return wrap("evidence", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return wrap("mark", err)
	}
	defer tx.Rollback()
	var current Wallet
	err = tx.QueryRowContext(ctx, `select id::text,user_id::text,address,coalesce(encrypted_private_key,''),key_version,chain_id,kind,`+fundedPredicate+`,migrated_at is not null,coalesce(did,''),coalesce(main_account_id,''),coalesce(layerx_key_id,'') from wallets w where id=$1::uuid for update`, w.ID).Scan(&current.ID, &current.UserID, &current.Address, &current.EncryptedPrivateKey, &current.KeyVersion, &current.ChainID, &current.Kind, &current.Funded, &current.Migrated, &current.DID, &current.MainAccountID, &current.OriginalIdentityKeyID)
	if err != nil || current.Funded || walletDigest(current) != journal.WalletDigest {
		return wrap("mark original identity", ErrMarkMigrated)
	}
	checked, e := loadOriginalIdentity(ctx, tx, current)
	if e != nil || !reflect.DeepEqual(checked, journal.Original) {
		return wrap("mark original producer", ErrMarkMigrated)
	}
	if current.Migrated {
		var secpID, edID string
		if err = tx.QueryRowContext(ctx, `select coalesce(attestor_key_id,''),coalesce(layerx_key_id,'') from wallets where id=$1`, w.ID).Scan(&secpID, &edID); err != nil || secpID != journal.Secp.KeyID || edID != original.KeyID {
			return wrap("mark identity", ErrMarkMigrated)
		}
	} else {
		res, e := tx.ExecContext(ctx, `update wallets set migrated_at=now(),attestor_key_id=$2 where id=$1::uuid and migrated_at is null`, w.ID, journal.Secp.KeyID)
		if e != nil {
			return wrap("mark", e)
		}
		n, e := res.RowsAffected()
		if e != nil || n != 1 {
			return wrap("mark", ErrMarkMigrated)
		}
	}
	if err = tx.Commit(); err != nil {
		return wrap("mark", err)
	}
	r.Matched++
	return nil
}

func newCurveJournal(keyID string, bundles []dealer.ShareBundle, pub *pt.ECPoint) (curveJournal, error) {
	c := curveJournal{KeyID: keyID}
	var err error
	c.PublicKey, err = attestor.PublicKeyHex(pub)
	if err != nil {
		return c, err
	}
	c.Curve, err = attestor.CurveName(bundles[0].Curve)
	if err != nil {
		return c, err
	}
	for _, out := range []*string{&c.ImportSession, &c.RefreshSession, &c.VerifySession} {
		*out, err = attestor.NewSessionID()
		if err != nil {
			return c, err
		}
	}
	for _, bundle := range bundles {
		encoded, e := attestor.EncodeBundle(bundle)
		if e != nil {
			return c, e
		}
		c.Shares = append(c.Shares, encoded)
	}
	return c, nil
}

func resumeCurve(ctx context.Context, client *attestor.Client, o Options, w Wallet, c *curveJournal, save func() error) error {
	curve, err := attestor.ParseCurve(c.Curve)
	if err != nil {
		return err
	}
	var bundles []dealer.ShareBundle
	defer func() { wipeBundles(bundles) }()
	for _, encoded := range c.Shares {
		b, e := attestor.DecodeBundle(encoded)
		if e != nil {
			return e
		}
		bundles = append(bundles, b)
	}
	if len(bundles) != 5 {
		return ErrJournal
	}
	pub := bundles[0].PublicKey
	actual, err := attestor.PublicKeyHex(pub)
	if err != nil || actual != c.PublicKey {
		return ErrJournal
	}
	if !c.Imported {
		_, err = client.ImportCeremony(ctx, o.CeremonyID, c.ImportSession, c.KeyID, w.UserID, w.Address, bundles, pub)
		if err != nil {
			return err
		}
		c.Imported = true
		if err = save(); err != nil {
			return err
		}
	}
	if !c.Refreshed {
		descriptions, e := client.DescribeCeremony(ctx, c.KeyID)
		if e != nil {
			return e
		}
		if e = prepareRefreshRecovery(descriptions, c, save); e != nil {
			return e
		}
		_, err = client.RefreshCeremony(ctx, o.CeremonyID, c.ImportSession, c.RefreshSession, c.RefreshRecoverySession, c.KeyID, pub, 0)
		if err != nil {
			return err
		}
		c.Refreshed = true
		c.Epoch = 1
		if err = save(); err != nil {
			return err
		}
	}
	if c.Verification == nil {
		descriptions, e := client.DescribeCeremony(ctx, c.KeyID)
		if e != nil {
			return e
		}
		rotate := true
		seen := false
		for _, node := range descriptions[:attestor.SignQuorum] {
			if node.VerificationSessionID == "" {
				rotate = false
				continue
			}
			seen = true
			if node.VerificationSessionID == c.RecoverySession && c.RecoverySession != "" {
				rotate = false
				continue
			}
			if node.VerificationSessionID != c.VerifySession {
				return attestor.ErrDisagree
			}
			if node.VerificationState != "failed" && node.VerificationState != "signing" {
				rotate = false
			}
		}
		if seen && rotate {
			c.RecoverySession = c.VerifySession
			c.VerifySession, e = attestor.NewSessionID()
			if e != nil {
				return e
			}
			if e = save(); e != nil {
				return e
			}
		}
		verification, e := client.SignVerificationCeremony(ctx, o.CeremonyID, c.VerifySession, c.RecoverySession, c.KeyID, c.ImportSession, pub, c.Epoch)
		if e != nil {
			return e
		}
		if verification.Curve != curve {
			return ErrJournal
		}
		c.Verification = &verification
		if err = save(); err != nil {
			return err
		}
	}
	return client.ReconcileCeremony(ctx, o.CeremonyID, c.ImportSession, c.RefreshSession, c.KeyID, c.PublicKey, c.Curve, c.Epoch)
}

func clearJournal(j *walletJournal) {
	for _, c := range []*curveJournal{&j.Secp, &j.Identity} {
		for i := range c.Shares {
			c.Shares[i].Share = ""
		}
		c.Shares = nil
	}
}
func wipeInt(v *big.Int) {
	if v == nil {
		return
	}
	for i := range v.Bits() {
		v.Bits()[i] = 0
	}
	v.SetInt64(0)
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

func completeCounts(r Report, n int) bool {
	return r.Read == n && r.Verified == n && r.Imported == n && r.Refreshed == n && r.TestSigned == n && r.Matched == n && r.HeldIdentity+r.ExternalIdentity == n
}

type originalIdentity struct {
	KeyID         string
	PublicKey     string
	Owner         string
	BindNonce     uint64
	BindSignature string
	External      bool
}

func mustHex(s string) []byte { raw, _ := hex.DecodeString(strings.TrimPrefix(s, "0x")); return raw }
func (i originalIdentity) message(w Wallet) []byte {
	out := append([]byte("LX:PAXEER-BIND:v1"), new(big.Int).SetInt64(w.ChainID).FillBytes(make([]byte, 32))...)
	out = append(out, common.HexToAddress(w.Address).Bytes()...)
	return binary.BigEndian.AppendUint64(out, i.BindNonce)
}

type identityQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadOriginalIdentity(ctx context.Context, db identityQuerier, w Wallet) (originalIdentity, error) {
	var out originalIdentity
	if !strings.HasPrefix(w.DID, "did:layerx:") || len(mustHex(strings.TrimPrefix(w.DID, "did:layerx:"))) != 32 || w.ChainID <= 0 {
		return out, ErrIdentityRecovery
	}
	var pub, keyID, did, main, nonce, signature, state string
	err := db.QueryRowContext(ctx, `select coalesce(ed_public_key,''),coalesce(ed_key_id,''),coalesce(did,''),coalesce(main_account_id,''),coalesce(bind_nonce::text,''),coalesce(bind_signature,''),state from account_provisioning where wallet_id=$1::uuid and user_id=$2::uuid and kind=$3`, w.ID, w.UserID, w.Kind).Scan(&pub, &keyID, &did, &main, &nonce, &signature, &state)
	if err != nil || did != w.DID || main != w.MainAccountID || pub != strings.TrimPrefix(w.DID, "did:layerx:") || keyID != w.OriginalIdentityKeyID || state != "active" {
		return out, ErrIdentityRecovery
	}
	n, err := strconv.ParseUint(nonce, 10, 64)
	if err != nil {
		return out, ErrIdentityRecovery
	}
	out = originalIdentity{KeyID: keyID, PublicKey: pub, Owner: w.UserID, BindNonce: n, BindSignature: strings.TrimPrefix(signature, "0x"), External: w.Kind == "agent"}
	if out.External {
		var registered, owner string
		if keyID != "" {
			return out, ErrIdentityRecovery
		}
		if err = db.QueryRowContext(ctx, `select public_key,coalesce(owner_user_id::text,'') from agent_principals where wallet_id=$1::uuid`, w.ID).Scan(&registered, &owner); err != nil || strings.TrimPrefix(registered, "0x") != pub {
			return out, ErrIdentityRecovery
		}
		out.Owner = owner
	} else if w.Kind != "standard" || keyID == "" {
		return out, ErrIdentityRecovery
	}
	if !ed25519.Verify(mustHex(pub), out.message(w), mustHex(signature)) {
		return out, ErrIdentitySig
	}
	name := []byte("agent:" + w.DID + ":main")
	preimage := append([]byte("LX:ACCOUNT:v1"), binary.BigEndian.AppendUint32(nil, uint32(len(name)))...)
	preimage = append(preimage, name...)
	expected := sha256.Sum256(preimage)
	if strings.TrimPrefix(w.MainAccountID, "0x") != hex.EncodeToString(expected[:]) {
		return out, ErrIdentityKey
	}
	return out, nil
}

func verifyOriginalBinding(ctx context.Context, rpcURL string, w Wallet, original originalIdentity, currentNonce *uint64) error {
	endpoint, err := url.Parse(rpcURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && (endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "localhost" || endpoint.Hostname() == "::1"))) {
		return ErrIdentityRecovery
	}
	call := func(method string, params any, out any) error {
		raw, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		if e != nil {
			return e
		}
		req, e := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(raw))
		if e != nil {
			return e
		}
		req.Header.Set("Content-Type", "application/json")
		client := http.Client{Timeout: 30 * time.Second}
		resp, e := client.Do(req)
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return ErrIdentityRecovery
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if e != nil {
			return e
		}
		if len(body) > 1<<20 {
			return ErrIdentityRecovery
		}
		var reply struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Result  json.RawMessage `json:"result"`
			Error   json.RawMessage `json:"error"`
		}
		if json.Unmarshal(body, &reply) != nil || reply.JSONRPC != "2.0" || reply.ID != 1 || len(reply.Error) > 0 && string(reply.Error) != "null" {
			return ErrIdentityRecovery
		}
		return json.Unmarshal(reply.Result, out)
	}
	var chainID string
	if err = call("eth_chainId", []any{}, &chainID); err != nil {
		return err
	}
	id, ok := new(big.Int).SetString(strings.TrimPrefix(chainID, "0x"), 16)
	if !ok || id.Cmp(big.NewInt(w.ChainID)) != 0 {
		return ErrIdentityKey
	}
	a, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"getUnifiedAccount","stateMutability":"view","inputs":[{"name":"evm","type":"address"}],"outputs":[{"name":"evm","type":"address"},{"name":"paxAddr","type":"string"},{"name":"didPublicKey","type":"bytes32"},{"name":"layerxMainAccountId","type":"bytes32"}]}]`))
	if err != nil {
		return err
	}
	data, err := a.Pack("getUnifiedAccount", common.HexToAddress(w.Address))
	if err != nil {
		return err
	}
	var result string
	if err = call("eth_call", []any{map[string]string{"to": "0x0000000000000000000000000000000000001004", "data": "0x" + hex.EncodeToString(data)}, "latest"}, &result); err != nil {
		return err
	}
	values, err := a.Unpack("getUnifiedAccount", mustHex(result))
	if err != nil || len(values) != 4 {
		return ErrIdentityKey
	}
	address, ok := values[0].(common.Address)
	if !ok || address != common.HexToAddress(w.Address) {
		return ErrIdentityKey
	}
	public, ok := values[2].([32]byte)
	if !ok || hex.EncodeToString(public[:]) != original.PublicKey {
		return ErrIdentityKey
	}
	main, ok := values[3].([32]byte)
	if !ok || hex.EncodeToString(main[:]) != strings.TrimPrefix(w.MainAccountID, "0x") {
		return ErrIdentityKey
	}
	nonceABI, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"layerXBindNonce","stateMutability":"view","inputs":[{"name":"evm","type":"address"}],"outputs":[{"name":"nonce","type":"uint64"}]}]`))
	if err != nil {
		return err
	}
	data, err = nonceABI.Pack("layerXBindNonce", common.HexToAddress(w.Address))
	if err != nil {
		return err
	}
	if err = call("eth_call", []any{map[string]string{"to": "0x0000000000000000000000000000000000001004", "data": "0x" + hex.EncodeToString(data)}, "latest"}, &result); err != nil {
		return err
	}
	values, err = nonceABI.Unpack("layerXBindNonce", mustHex(result))
	if err != nil || len(values) != 1 {
		return ErrIdentityKey
	}
	nonce, ok := values[0].(uint64)
	if !ok {
		return ErrIdentityKey
	}
	*currentNonce = nonce
	return nil
}

func resumeHeldIdentity(ctx context.Context, client *attestor.Client, o Options, w Wallet, c *curveJournal, original originalIdentity, save func() error) error {
	replies, err := client.DescribeCeremony(ctx, c.KeyID)
	if err != nil {
		return err
	}
	for _, reply := range replies {
		if reply.PublicKey != original.PublicKey || reply.Curve != attestor.CurveEd25519 || reply.Owner != original.Owner || !strings.EqualFold(reply.Account, w.Address) {
			return attestor.ErrDisagree
		}
	}
	if !c.EpochCaptured {
		c.BaseEpoch = replies[0].Epoch
		for _, reply := range replies {
			if reply.Epoch != c.BaseEpoch || reply.RefreshSessionID != "" && reply.RefreshState != "complete" && reply.RefreshSessionID != c.RefreshSession {
				return attestor.ErrDisagree
			}
		}
		c.EpochCaptured = true
		if err = save(); err != nil {
			return err
		}
	}
	if c.Ownership == nil {
		proof, e := client.SignOriginalBinding(ctx, c.VerifySession, c.KeyID, original.Owner, mustHex(original.PublicKey), c.PossessionMessage)
		if e != nil {
			return retainOwnerRetry(c, save, e)
		}
		c.Ownership = &proof
		c.VerifySession, e = attestor.NewSessionID()
		if e != nil {
			return e
		}
		if e = save(); e != nil {
			return e
		}
	}
	if !c.Refreshed {
		for _, reply := range replies {
			if reply.Epoch != c.BaseEpoch && !(reply.Epoch == c.BaseEpoch+1 && reply.RefreshSessionID == c.RefreshSession) {
				return attestor.ErrDisagree
			}
		}
		pub, e := attestor.DecodePoint(dealer.Ed25519, original.PublicKey)
		if e != nil {
			return e
		}
		if err = prepareRefreshRecovery(replies, c, save); err != nil {
			return err
		}
		if _, err = client.RefreshOriginalIdentity(ctx, o.CeremonyID, c.RefreshSession, c.RefreshRecoverySession, c.KeyID, pub, c.BaseEpoch); err != nil {
			return err
		}
		c.Epoch = c.BaseEpoch + 1
		c.Refreshed = true
		if err = save(); err != nil {
			return err
		}
	}
	replies, err = client.DescribeCeremony(ctx, c.KeyID)
	if err != nil {
		return err
	}
	for _, reply := range replies {
		if reply.Epoch != c.Epoch || reply.RefreshSessionID != c.RefreshSession || reply.RefreshState != "complete" || reply.PublicKey != original.PublicKey {
			return attestor.ErrDisagree
		}
	}
	if c.Verification == nil {
		proof, e := client.SignOriginalBinding(ctx, c.VerifySession, c.KeyID, original.Owner, mustHex(original.PublicKey), c.PossessionMessage)
		if e != nil {
			return retainOwnerRetry(c, save, e)
		}
		c.Verification = &proof
		if err = save(); err != nil {
			return err
		}
	}
	return nil
}

func prepareRefreshRecovery(replies []attestor.DescribeResponse, c *curveJournal, save func() error) error {
	aborted := false
	for _, reply := range replies {
		if reply.RefreshSessionID == c.RefreshSession && reply.RefreshState == "aborted" {
			aborted = true
		}
	}
	if !aborted {
		return nil
	}
	for _, reply := range replies {
		if reply.RefreshState == "complete" || reply.Epoch > c.BaseEpoch {
			return attestor.ErrDisagree
		}
		if reply.RefreshSessionID != "" && reply.RefreshSessionID != c.RefreshSession {
			return attestor.ErrDisagree
		}
	}
	c.RefreshRecoverySession = c.RefreshSession
	var err error
	c.RefreshSession, err = attestor.NewSessionID()
	if err != nil {
		return err
	}
	return save()
}

func retainOwnerRetry(c *curveJournal, save func() error, cause error) error {
	var api *attestor.APIError
	if errors.As(cause, &api) && (api.Code == "session_open_failed" || api.Code == "session_failed" || api.Code == "session_timeout" || api.Code == "token_invalid") {
		session, err := attestor.NewSessionID()
		if err != nil {
			return err
		}
		c.VerifySession = session
		if err = save(); err != nil {
			return err
		}
	}
	return cause
}

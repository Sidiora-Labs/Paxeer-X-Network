package archive

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/scrypt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/migrate"
)

const (
	EnvPassphrase    = "CEREMONY_ARCHIVE_PASSPHRASE"
	EnvPath          = "CEREMONY_ARCHIVE_PATH"
	MinPassphraseLen = 16
	magic            = "PXWARCH1"
	saltLen          = 16
	nonceLen         = 12
	keyLen           = 32
	scryptLogN       = 15
	scryptR          = 8
	scryptP          = 1
	headerLen        = len(magic) + 3 + saltLen + nonceLen
	maxArchiveSize   = 1 << 30
)

var (
	ErrPassphrase = errors.New("archive: passphrase is missing or shorter than 16 bytes")
	ErrPath       = errors.New("archive: archive path is not set")
	ErrFormat     = errors.New("archive: malformed archive file")
	ErrDecrypt    = errors.New("archive: archive does not decrypt under the passphrase")
	ErrVerify     = errors.New("archive: archive rows differ from the database rows")
)

type Result struct {
	Path string
	Rows int
}

type payload struct {
	Table string            `json:"table"`
	Rows  []json.RawMessage `json:"rows"`
}

func LoadPassphrase(getenv func(string) string) ([]byte, error) {
	p := getenv(EnvPassphrase)
	if len(p) < MinPassphraseLen {
		return nil, fmt.Errorf("%w: %s", ErrPassphrase, EnvPassphrase)
	}
	return []byte(p), nil
}

func LoadPath(getenv func(string) string) (string, error) {
	p := strings.TrimSpace(getenv(EnvPath))
	if p == "" {
		return "", fmt.Errorf("%w: %s", ErrPath, EnvPath)
	}
	return p, nil
}

func ReadFundedRows(ctx context.Context, db *sql.DB) ([]json.RawMessage, error) {
	rows, err := db.QueryContext(ctx, `select row_to_json(w)::text from wallets w where `+migrate.FundedPredicate()+` order by w.id`)
	if err != nil {
		return nil, fmt.Errorf("archive: read funded wallets: %w", err)
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("archive: scan funded wallet: %w", err)
		}
		out = append(out, json.RawMessage(s))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("archive: read funded wallets: %w", err)
	}
	return out, nil
}

func Archive(ctx context.Context, db *sql.DB, path string, passphrase []byte) (Result, error) {
	if len(passphrase) < MinPassphraseLen {
		return Result{}, ErrPassphrase
	}
	if path == "" {
		return Result{}, ErrPath
	}
	rows, err := ReadFundedRows(ctx, db)
	if err != nil {
		return Result{}, err
	}
	plain, err := json.Marshal(payload{Table: "wallets", Rows: rows})
	if err != nil {
		return Result{}, err
	}
	sealed, err := Seal(plain, passphrase)
	zero(plain)
	if err != nil {
		return Result{}, err
	}
	if err := writeExclusive(path, sealed); err != nil {
		return Result{}, err
	}
	back, err := Open(path, passphrase)
	if err != nil {
		return Result{}, err
	}
	if err := sameRows(rows, back); err != nil {
		return Result{}, err
	}
	return Verify(ctx, db, path, passphrase)
}

func Verify(ctx context.Context, db *sql.DB, path string, passphrase []byte) (Result, error) {
	back, err := Open(path, passphrase)
	if err != nil {
		return Result{}, err
	}
	fresh, err := ReadFundedRows(ctx, db)
	if err != nil {
		return Result{}, err
	}
	if err := sameRows(fresh, back); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Rows: len(back)}, nil
}

func Seal(plain, passphrase []byte) ([]byte, error) {
	header := make([]byte, headerLen)
	copy(header, magic)
	header[len(magic)] = scryptLogN
	header[len(magic)+1] = scryptR
	header[len(magic)+2] = scryptP
	salt := header[len(magic)+3 : len(magic)+3+saltLen]
	nonce := header[len(magic)+3+saltLen:]
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	gcm, err := newGCM(passphrase, salt, scryptLogN, scryptR, scryptP)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(header, nonce, plain, header), nil
}

func Unseal(data, passphrase []byte) ([]byte, error) {
	if len(data) < headerLen+16 || string(data[:len(magic)]) != magic {
		return nil, ErrFormat
	}
	logN, r, p := data[len(magic)], data[len(magic)+1], data[len(magic)+2]
	if logN < 14 || logN > 22 || r == 0 || r > 32 || p == 0 || p > 16 {
		return nil, ErrFormat
	}
	header := data[:headerLen]
	salt := header[len(magic)+3 : len(magic)+3+saltLen]
	nonce := header[len(magic)+3+saltLen:]
	gcm, err := newGCM(passphrase, salt, int(logN), int(r), int(p))
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, data[headerLen:], header)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}

func Open(path string, passphrase []byte) ([]json.RawMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("archive: open: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxArchiveSize+1))
	if err != nil {
		return nil, fmt.Errorf("archive: read: %w", err)
	}
	if len(data) > maxArchiveSize {
		return nil, ErrFormat
	}
	plain, err := Unseal(data, passphrase)
	if err != nil {
		return nil, err
	}
	defer zero(plain)
	var p payload
	if err := json.Unmarshal(plain, &p); err != nil || p.Table != "wallets" {
		return nil, ErrFormat
	}
	return p.Rows, nil
}

func newGCM(passphrase, salt []byte, logN, r, p int) (cipher.AEAD, error) {
	key, err := scrypt.Key(passphrase, salt, 1<<logN, r, p, keyLen)
	if err != nil {
		return nil, err
	}
	defer zero(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithNonceSize(block, nonceLen)
}

func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("archive: create: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("archive: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("archive: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("archive: close: %w", err)
	}
	return nil
}

func sameRows(want, got []json.RawMessage) error {
	if len(want) != len(got) {
		return fmt.Errorf("%w: %d rows in the database, %d in the archive", ErrVerify, len(want), len(got))
	}
	for i := range want {
		var a, b bytes.Buffer
		if err := json.Compact(&a, want[i]); err != nil {
			return fmt.Errorf("%w: row %d", ErrVerify, i)
		}
		if err := json.Compact(&b, got[i]); err != nil {
			return fmt.Errorf("%w: row %d", ErrVerify, i)
		}
		if !bytes.Equal(a.Bytes(), b.Bytes()) {
			return fmt.Errorf("%w: row %d", ErrVerify, i)
		}
	}
	return nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

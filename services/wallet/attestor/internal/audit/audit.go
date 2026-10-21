package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	FileName       = "audit.log"
	maxRecordBytes = 1 << 20
	lengthBytes    = 4
)

var (
	ErrClosed        = errors.New("audit: log closed")
	ErrFieldTooLong  = errors.New("audit: field exceeds 65535 bytes")
	ErrLogFailed     = errors.New("audit: log failed on an earlier append")
	ErrHeadMismatch  = errors.New("audit: replayed head differs from the open log's head")
	errTruncated     = errors.New("truncated record")
	errOversized     = errors.New("record length out of range")
	errMalformed     = errors.New("malformed record")
	errSequence      = errors.New("sequence out of order")
	errBrokenLink    = errors.New("previous hash does not match the preceding record")
	errHashMismatch  = errors.New("record hash does not match its contents")
	errTrailingBytes = errors.New("trailing bytes inside record")
)

type Entry struct {
	Kind      string
	KeyID     string
	Subject   []byte
	Decision  string
	Reason    string
	SessionID string
}

type Record struct {
	Sequence    uint64
	UnixNano    int64
	PrevHash    [32]byte
	Kind        string
	KeyID       string
	SubjectHash [32]byte
	Decision    string
	Reason      string
	SessionID   string
	Hash        [32]byte
}

type CorruptionError struct {
	Sequence uint64
	Offset   int64
	Err      error
}

func (e *CorruptionError) Error() string {
	return fmt.Sprintf("audit: record %d at offset %d: %v", e.Sequence, e.Offset, e.Err)
}

func (e *CorruptionError) Unwrap() error { return e.Err }

type Log struct {
	mu       sync.Mutex
	path     string
	file     *os.File
	sequence uint64
	head     [32]byte
	failed   bool
	closed   bool
}

func Open(dir string) (*Log, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("audit: create data directory: %w", err)
	}
	path := filepath.Join(dir, FileName)
	_, statErr := os.Stat(path)
	created := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !created {
		return nil, fmt.Errorf("audit: stat log: %w", statErr)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open log: %w", err)
	}
	if created {
		if err := syncDir(dir); err != nil {
			file.Close()
			return nil, err
		}
	}
	sequence, head, err := replay(path)
	if err != nil {
		file.Close()
		return nil, err
	}
	return &Log{path: path, file: file, sequence: sequence, head: head}, nil
}

func (l *Log) Append(e Entry) (Record, error) {
	for _, field := range []string{e.Kind, e.KeyID, e.Decision, e.Reason, e.SessionID} {
		if len(field) > math.MaxUint16 {
			return Record{}, ErrFieldTooLong
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return Record{}, ErrClosed
	}
	if l.failed {
		return Record{}, ErrLogFailed
	}
	rec := Record{
		Sequence:    l.sequence + 1,
		UnixNano:    time.Now().UnixNano(),
		PrevHash:    l.head,
		Kind:        e.Kind,
		KeyID:       e.KeyID,
		SubjectHash: sha256.Sum256(e.Subject),
		Decision:    e.Decision,
		Reason:      e.Reason,
		SessionID:   e.SessionID,
	}
	body := encodeBody(rec)
	rec.Hash = sha256.Sum256(body)
	frame := make([]byte, lengthBytes, lengthBytes+len(body)+len(rec.Hash))
	binary.BigEndian.PutUint32(frame, uint32(len(body)+len(rec.Hash)))
	frame = append(frame, body...)
	frame = append(frame, rec.Hash[:]...)
	if _, err := l.file.Write(frame); err != nil {
		l.failed = true
		return Record{}, fmt.Errorf("audit: write record %d: %w", rec.Sequence, err)
	}
	if err := l.file.Sync(); err != nil {
		l.failed = true
		return Record{}, fmt.Errorf("audit: sync record %d: %w", rec.Sequence, err)
	}
	l.sequence = rec.Sequence
	l.head = rec.Hash
	return rec, nil
}

func (l *Log) Head() (uint64, [32]byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sequence, l.head
}

func (l *Log) Verify() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	sequence, head, err := replay(l.path)
	if err != nil {
		return err
	}
	if sequence != l.sequence || head != l.head {
		return fmt.Errorf("%w: file ends at %d, log at %d", ErrHeadMismatch, sequence, l.sequence)
	}
	return nil
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.file.Close()
}

func replay(path string) (uint64, [32]byte, error) {
	var head [32]byte
	file, err := os.Open(path)
	if err != nil {
		return 0, head, fmt.Errorf("audit: open log for replay: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var sequence uint64
	var offset int64
	var prefix [lengthBytes]byte
	for {
		expected := sequence + 1
		_, err := io.ReadFull(reader, prefix[:])
		if err == io.EOF {
			return sequence, head, nil
		}
		if err != nil {
			return 0, head, &CorruptionError{Sequence: expected, Offset: offset, Err: errTruncated}
		}
		size := binary.BigEndian.Uint32(prefix[:])
		if size <= 32 || size > maxRecordBytes {
			return 0, head, &CorruptionError{Sequence: expected, Offset: offset, Err: errOversized}
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return 0, head, &CorruptionError{Sequence: expected, Offset: offset, Err: errTruncated}
		}
		body := payload[:len(payload)-32]
		var stored [32]byte
		copy(stored[:], payload[len(payload)-32:])
		if sha256.Sum256(body) != stored {
			return 0, head, &CorruptionError{Sequence: expected, Offset: offset, Err: errHashMismatch}
		}
		rec, err := decodeBody(body)
		if err != nil {
			return 0, head, &CorruptionError{Sequence: expected, Offset: offset, Err: err}
		}
		if rec.Sequence != expected {
			return 0, head, &CorruptionError{Sequence: expected, Offset: offset, Err: errSequence}
		}
		if rec.PrevHash != head {
			return 0, head, &CorruptionError{Sequence: expected, Offset: offset, Err: errBrokenLink}
		}
		sequence = rec.Sequence
		head = stored
		offset += int64(lengthBytes) + int64(size)
	}
}

func encodeBody(r Record) []byte {
	size := 8 + 8 + 32 + 32
	for _, field := range []string{r.Kind, r.KeyID, r.Decision, r.Reason, r.SessionID} {
		size += 2 + len(field)
	}
	buf := make([]byte, 0, size)
	buf = binary.BigEndian.AppendUint64(buf, r.Sequence)
	buf = binary.BigEndian.AppendUint64(buf, uint64(r.UnixNano))
	buf = append(buf, r.PrevHash[:]...)
	buf = appendString(buf, r.Kind)
	buf = appendString(buf, r.KeyID)
	buf = append(buf, r.SubjectHash[:]...)
	buf = appendString(buf, r.Decision)
	buf = appendString(buf, r.Reason)
	buf = appendString(buf, r.SessionID)
	return buf
}

func appendString(buf []byte, s string) []byte {
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(s)))
	return append(buf, s...)
}

type decoder struct {
	buf []byte
	err error
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if len(d.buf) < n {
		d.err = errMalformed
		return nil
	}
	out := d.buf[:n]
	d.buf = d.buf[n:]
	return out
}

func (d *decoder) uint64() uint64 {
	b := d.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func (d *decoder) hash() [32]byte {
	var out [32]byte
	copy(out[:], d.take(32))
	return out
}

func (d *decoder) string() string {
	b := d.take(2)
	if b == nil {
		return ""
	}
	return string(d.take(int(binary.BigEndian.Uint16(b))))
}

func decodeBody(body []byte) (Record, error) {
	d := &decoder{buf: body}
	var r Record
	r.Sequence = d.uint64()
	r.UnixNano = int64(d.uint64())
	r.PrevHash = d.hash()
	r.Kind = d.string()
	r.KeyID = d.string()
	r.SubjectHash = d.hash()
	r.Decision = d.string()
	r.Reason = d.string()
	r.SessionID = d.string()
	if d.err != nil {
		return Record{}, d.err
	}
	if len(d.buf) != 0 {
		return Record{}, errTrailingBytes
	}
	r.Hash = sha256.Sum256(body)
	return r, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("audit: open data directory: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("audit: sync data directory: %w", err)
	}
	return nil
}

package s3

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	tmtypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
)

// EmitResult reports what was published: the hex SHA-256 over the uncompressed
// payload, surfaced in the TaskResult/log so an operator can verify the
// decompressed object after gunzip. It is the canonical reader-verifiable seal;
// it is not embedded in the object itself (a payload cannot carry the hash of
// its own bytes).
type EmitResult struct {
	// UncompressedSHA256 is the hex digest of the bytes fed into gzip.
	UncompressedSHA256 string
}

// StreamGzipNDJSON gzips each record as one NDJSON line and streams it to S3
// without buffering the whole payload, computing a SHA-256 over the
// uncompressed bytes as they pass. The integrity seal is twofold:
//
//   - ChecksumAlgorithm=SHA256 on the put: the SDK sends a trailing aws-chunked
//     checksum, so S3 validates a SHA-256 of the compressed bytes it received
//     without precomputing — the wire seal. For a multipart upload (payload
//     over the uploader's threshold) S3 stores the composite-of-parts form
//     (`<hash>-N`), still a valid per-part seal but not a flat SHA-256 of the
//     body a reader can recompute.
//   - the returned UncompressedSHA256: the logical seal over the pre-gzip
//     payload, surfaced via EmitResult so a reader verifies the decompressed
//     bytes independently. This, not the S3-side checksum, is the canonical
//     reader-verifiable seal.
//
// io.Pipe backpressure is preserved: the marshaling goroutine blocks on the
// uploader's reads, so memory stays bounded to the uploader's part-buffer pool
// (~part_size × (concurrency+1)), independent of total payload size.
func StreamGzipNDJSON[T any](ctx context.Context, uploader Uploader, bucket, key string, records []T) (EmitResult, error) {
	return streamGzip(ctx, uploader, bucket, key, func(w io.Writer) error {
		return encodeNDJSON(w, records)
	})
}

// StreamGzipJSON gzips a single indented JSON object and streams it to S3 with
// the same uncompressed-payload seal as StreamGzipNDJSON.
func StreamGzipJSON(ctx context.Context, uploader Uploader, bucket, key string, obj any) (EmitResult, error) {
	return streamGzip(ctx, uploader, bucket, key, func(w io.Writer) error {
		return encodeIndentedJSON(w, obj)
	})
}

// StreamGzipFunc gzips and streams whatever write emits, under the same seal as
// StreamGzipNDJSON. It exists for producers that generate the payload lazily
// (e.g. querying one block at a time) and so must not materialize the whole
// record set in memory first. write receives the destination writer and must
// emit the complete uncompressed payload.
func StreamGzipFunc(ctx context.Context, uploader Uploader, bucket, key string, write func(io.Writer) error) (EmitResult, error) {
	return streamGzip(ctx, uploader, bucket, key, write)
}

// streamGzip wires the io.Pipe -> hash-tee -> gzip pipeline and the S3 upload.
// write emits the uncompressed payload into the supplied writer.
func streamGzip(ctx context.Context, uploader Uploader, bucket, key string, write func(io.Writer) error) (EmitResult, error) {
	pr, pw := io.Pipe()
	h := sha256.New()

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- writeGzip(pw, h, write)
	}()

	_, uploadErr := uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(key),
		Body:              pr,
		ContentType:       aws.String("application/gzip"),
		ChecksumAlgorithm: tmtypes.ChecksumAlgorithmSha256,
	})
	if uploadErr != nil {
		// Unblock the writer goroutine if the upload aborted early.
		pr.CloseWithError(uploadErr)
	}

	wErr := <-writeErr
	if uploadErr != nil {
		return EmitResult{}, uploadErr
	}
	if wErr != nil {
		return EmitResult{}, wErr
	}
	return EmitResult{UncompressedSHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// writeGzip tees the uncompressed payload through h (the integrity hash) while
// gzipping it into the pipe. The pipe is closed with the terminal error so the
// uploader observes it instead of a truncated, silently-valid object.
func writeGzip(pw *io.PipeWriter, h hash.Hash, write func(io.Writer) error) (retErr error) {
	defer func() {
		if retErr != nil {
			pw.CloseWithError(retErr)
		} else {
			_ = pw.Close()
		}
	}()

	gw := gzip.NewWriter(pw)
	defer func() {
		if err := gw.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("closing gzip writer: %w", err)
		}
	}()

	// Registered last so it runs first: a panic in write (e.g. a record's
	// MarshalJSON over attacker-adjacent chain data) becomes retErr, so the
	// pipe-close defer does CloseWithError — the uploader aborts instead of
	// publishing a truncated object, and the panic never escapes this
	// task-spawned goroutine to crash the sidecar.
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("panic in payload writer: %v", r)
		}
	}()

	return write(io.MultiWriter(gw, h))
}

func encodeNDJSON[T any](w io.Writer, records []T) error {
	enc := json.NewEncoder(w)
	for i := range records {
		// Encoder.Encode appends '\n', yielding one record per line.
		if err := enc.Encode(records[i]); err != nil {
			return fmt.Errorf("marshaling record %d: %w", i, err)
		}
	}
	return nil
}

func encodeIndentedJSON(w io.Writer, obj any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(obj)
}

package image

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const workerArgument = "--kindle-raster-worker-v2"

// ProcessWorker decodes untrusted rasters in a separate process (this binary
// re-executed with workerArgument) under kernel resource ceilings. Isolation
// selects how the ceilings are imposed:
//
//   - "" or "cgroup": a fresh delegated cgroup v2 group under CgroupParent
//     (memory.max 256 MiB, no swap, pids.max 32, one CPU).
//   - "rlimit": hard per-process resource limits (address space, CPU time,
//     open files, file size, core dumps) set on the worker by the parent with
//     prlimit(2) and read back before any untrusted byte is written to it,
//     for platforms that do not delegate cgroups to containers.
//
// Both modes keep the separate address space, the five-second wall deadline,
// the bounded output and the output validation.
type ProcessWorker struct {
	Executable, CgroupParent string
	Isolation                string
}

// RlimitIsolation is the ProcessWorker isolation mode backed by prlimit(2).
const RlimitIsolation = "rlimit"

type workerResponse struct {
	Raster  Raster
	Failure string
}
type boundedWorkerOutput struct{ bytes.Buffer }

func (b *boundedWorkerOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 26<<20 {
		return 0, ErrLimit
	}
	return b.Buffer.Write(p)
}

func init() {
	if len(os.Args) != 2 || os.Args[1] != workerArgument {
		return
	}
	body, e := ReadBounded(os.Stdin)
	response := workerResponse{}
	if e == nil {
		response.Raster, e = ReencodeSupported(context.Background(), body)
	}
	if e != nil {
		switch {
		case errors.Is(e, ErrLimit):
			response.Failure = "limit"
		case errors.Is(e, ErrUnavailable):
			response.Failure = "unavailable"
		default:
			response.Failure = "invalid"
		}
	}
	if json.NewEncoder(os.Stdout).Encode(response) != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func (w ProcessWorker) Ready() error {
	if w.Isolation != "" && w.Isolation != "cgroup" && w.Isolation != RlimitIsolation {
		return ErrUnavailable
	}
	if !filepath.IsAbs(w.Executable) || strings.ContainsAny(w.Executable+w.CgroupParent, "\r\n") {
		return ErrUnavailable
	}
	info, e := os.Stat(w.Executable)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return ErrUnavailable
	}
	if w.Isolation == RlimitIsolation {
		return rlimitSupported()
	}
	if !filepath.IsAbs(w.CgroupParent) {
		return ErrUnavailable
	}
	var filesystem syscall.Statfs_t
	if syscall.Statfs(w.CgroupParent, &filesystem) != nil || uint64(filesystem.Type) != 0x63677270 {
		return ErrUnavailable
	}
	raw, e := os.ReadFile(filepath.Join(w.CgroupParent, "cgroup.controllers"))
	if e != nil {
		return ErrUnavailable
	}
	fields := " " + strings.Join(strings.Fields(string(raw)), " ") + " "
	for _, name := range []string{"memory", "cpu", "pids"} {
		if !strings.Contains(fields, " "+name+" ") {
			return ErrUnavailable
		}
	}
	control, e := os.ReadFile(filepath.Join(w.CgroupParent, "cgroup.subtree_control"))
	if e != nil {
		return ErrUnavailable
	}
	enabled := " " + strings.Join(strings.Fields(string(control)), " ") + " "
	for _, name := range []string{"memory", "cpu", "pids"} {
		if !strings.Contains(enabled, " "+name+" ") {
			return ErrUnavailable
		}
	}
	return nil
}
func (w ProcessWorker) Decode(ctx context.Context, body []byte) (Raster, error) {
	if len(body) > MaxBytes {
		return Raster{}, ErrLimit
	}
	if e := w.Ready(); e != nil {
		return Raster{}, e
	}
	rlimit := w.Isolation == RlimitIsolation
	group := ""
	if !rlimit {
		nonce := make([]byte, 16)
		if _, e := rand.Read(nonce); e != nil {
			return Raster{}, ErrUnavailable
		}
		group = filepath.Join(w.CgroupParent, "metadata-raster-"+hex.EncodeToString(nonce))
		if os.Mkdir(group, 0700) != nil {
			return Raster{}, ErrUnavailable
		}
		defer os.Remove(group)
		for name, value := range map[string]string{"memory.max": "268435456", "memory.swap.max": "0", "pids.max": "32", "cpu.max": "100000 100000"} {
			if os.WriteFile(filepath.Join(group, name), []byte(value), 0600) != nil {
				return Raster{}, ErrUnavailable
			}
			actual, e := os.ReadFile(filepath.Join(group, name))
			if e != nil || strings.TrimSpace(string(actual)) != value {
				return Raster{}, ErrUnavailable
			}
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, w.Executable, workerArgument)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "GOMAXPROCS=1", "GOMEMLIMIT=192MiB"}
	cmd.SysProcAttr = workerProcAttr()
	input, e := cmd.StdinPipe()
	if e != nil {
		return Raster{}, ErrUnavailable
	}
	var output boundedWorkerOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if cmd.Start() != nil {
		input.Close()
		return Raster{}, ErrUnavailable
	}
	joined := false
	defer func() {
		if !joined {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	if rlimit {
		// The worker blocks on stdin until the parent writes the image, so
		// the limits are in force before any untrusted byte reaches it.
		if applyRasterLimits(cmd.Process.Pid) != nil {
			input.Close()
			return Raster{}, ErrUnavailable
		}
	} else {
		if os.WriteFile(filepath.Join(group, "cgroup.procs"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600) != nil {
			input.Close()
			return Raster{}, ErrUnavailable
		}
		membership, e := os.ReadFile(filepath.Join(group, "cgroup.procs"))
		if e != nil || !strings.Contains("\n"+string(membership), "\n"+strconv.Itoa(cmd.Process.Pid)+"\n") {
			input.Close()
			return Raster{}, ErrUnavailable
		}
	}
	writeErr := make(chan error, 1)
	go func() {
		_, e := io.Copy(input, bytes.NewReader(bytes.Clone(body)))
		closeErr := input.Close()
		if e == nil {
			e = closeErr
		}
		writeErr <- e
	}()
	e = cmd.Wait()
	joined = true
	if bounded.Err() != nil {
		return Raster{}, ErrLimit
	}
	if e != nil {
		return Raster{}, ErrLimit
	}
	if <-writeErr != nil {
		return Raster{}, ErrUnavailable
	}
	var result workerResponse
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF {
		return Raster{}, ErrInvalid
	}
	switch result.Failure {
	case "limit":
		return Raster{}, ErrLimit
	case "unavailable":
		return Raster{}, ErrUnavailable
	case "invalid":
		return Raster{}, ErrInvalid
	case "":
	default:
		return Raster{}, ErrInvalid
	}
	if result.Raster.MIME != "image/png" || result.Raster.Width < 1 || result.Raster.Height < 1 || result.Raster.Width > 8192 || result.Raster.Height > 8192 || int64(result.Raster.Width)*int64(result.Raster.Height) > 20000000 || len(result.Raster.Variants) != 3 {
		return Raster{}, ErrInvalid
	}
	for _, n := range []int{256, 512, 1024} {
		b := result.Raster.Variants[n]
		if len(b) == 0 || len(b) > MaxBytes {
			return Raster{}, ErrInvalid
		}
	}
	return result.Raster, nil
}

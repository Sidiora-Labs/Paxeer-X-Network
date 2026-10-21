//go:build linux

package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func rlimitWorker(t *testing.T) ProcessWorker {
	t.Helper()
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	worker := ProcessWorker{Executable: executable, Isolation: RlimitIsolation}
	if e = worker.Ready(); e != nil {
		t.Fatal("rlimit isolation must be available on linux:", e)
	}
	return worker
}

func TestProductionRlimitWorkerReadiness(t *testing.T) {
	if (ProcessWorker{Isolation: RlimitIsolation}).Ready() != ErrUnavailable {
		t.Fatal("rlimit worker without executable must be unavailable")
	}
	if (ProcessWorker{Executable: "/nonexistent/worker", Isolation: RlimitIsolation}).Ready() != ErrUnavailable {
		t.Fatal("missing executable must be unavailable")
	}
	executable, _ := os.Executable()
	if (ProcessWorker{Executable: executable, Isolation: "none"}).Ready() != ErrUnavailable {
		t.Fatal("unknown isolation must be unavailable")
	}
	// cgroup mode keeps requiring a delegated cgroup parent.
	if (ProcessWorker{Executable: executable}).Ready() != ErrUnavailable {
		t.Fatal("cgroup isolation without a parent must be unavailable")
	}
	rlimitWorker(t)
}

func TestProductionRlimitWorkerDecodesInSeparateProcess(t *testing.T) {
	worker := rlimitWorker(t)
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 40, 30))); e != nil {
		t.Fatal(e)
	}
	out, e := worker.Decode(context.Background(), b.Bytes())
	if e != nil || out.MIME != "image/png" || out.Width != 40 || out.Height != 30 || len(out.Variants) != 3 {
		t.Fatalf("%+v %v", out, e)
	}
	for _, size := range []int{256, 512, 1024} {
		if _, e := png.Decode(bytes.NewReader(out.Variants[size])); e != nil {
			t.Fatal(size, e)
		}
	}
	if _, e = worker.Decode(context.Background(), []byte("not an image")); e != ErrInvalid {
		t.Fatal("garbage must be rejected as invalid:", e)
	}
}

// The largest raster the policy admits (20 MP) must decode within the
// ceilings; the limits bound abuse, not legitimate uploads.
func TestProductionRlimitWorkerAdmitsLargestPolicyRaster(t *testing.T) {
	worker := rlimitWorker(t)
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 5000, 4000))); e != nil {
		t.Fatal(e)
	}
	out, e := worker.Decode(context.Background(), b.Bytes())
	if e != nil || out.Width != 5000 || out.Height != 4000 {
		t.Fatalf("%v %v", out.Width, e)
	}
}

func TestProductionRlimitWorkerDimensionBomb(t *testing.T) {
	worker := rlimitWorker(t)
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 1, 1))); e != nil {
		t.Fatal(e)
	}
	bomb := b.Bytes()
	binary.BigEndian.PutUint32(bomb[16:20], 8193)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	result, e := worker.Decode(context.Background(), bomb)
	if e != ErrLimit || len(result.Variants) != 0 {
		t.Fatal("dimension bomb escaped the worker limit", e, len(result.Variants))
	}
}

// The ceilings are set on the live worker before it receives input and are
// visible to the kernel exactly as declared.
func TestProductionRlimitCeilingsAppliedToWorker(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(executable, workerArgument)
	cmd.Env = []string{"GOMAXPROCS=1"}
	stdin, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { stdin.Close(); cmd.Wait() }()
	if e = applyRasterLimits(cmd.Process.Pid); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/limits")
	if e != nil {
		t.Fatal(e)
	}
	limits := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasPrefix(line, "Max ") {
			continue
		}
		// "Max address space  1073741824  1073741824  bytes"
		name := strings.TrimSpace(line[:26])
		limits[name] = strings.Join(strings.Fields(line[26:])[:2], " ")
	}
	for name, want := range map[string]string{"Max data size": "536870912 536870912", "Max address space": "4294967296 4294967296", "Max cpu time": "5 5", "Max open files": "16 16", "Max file size": "0 0", "Max core file size": "0 0"} {
		if limits[name] != want {
			t.Fatalf("%s = %q, want %q (%v)", name, limits[name], want, limits)
		}
	}
	if applyRasterLimits(0) != ErrUnavailable || applyRasterLimits(1) != ErrUnavailable {
		t.Fatal("limits must never target this process or init")
	}
}

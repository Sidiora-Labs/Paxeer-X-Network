package fonts

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	RegularSHA256 = "40d692fce188e4471e2b3cba937be967878f631ad3ebbbdcd587687c7ebe0c82"
	BoldSHA256    = "288316099b1e0a47a4716d159098005eef7c0066921f34e3200393dbdb01947f"
	LicenseSHA256 = "262481e844521b326f5ecd053e59b98c8b2da78c8ee1bdbb6e8174305e54935a"
)

//go:embed Inter-Regular.ttf Inter-Bold.ttf OFL.txt
var embedded embed.FS

type asset struct {
	name   string
	digest string
}

func Materialize(dest string) error {
	if !filepath.IsAbs(dest) || filepath.Clean(dest) != dest || dest == string(filepath.Separator) {
		return fmt.Errorf("fonts: destination must be an explicit clean absolute directory")
	}
	directory, err := openPrivateDirectory(dest)
	if err != nil {
		return err
	}
	defer directory.Close()
	for _, item := range []asset{{"Inter-Regular.ttf", RegularSHA256}, {"Inter-Bold.ttf", BoldSHA256}, {"OFL.txt", LicenseSHA256}} {
		data, err := embedded.ReadFile(item.name)
		if err != nil {
			return fmt.Errorf("fonts: embedded asset %s: %w", item.name, err)
		}
		if digest(data) != item.digest {
			return fmt.Errorf("fonts: embedded asset %s digest mismatch", item.name)
		}
		if err := installAsset(directory, item, data); err != nil {
			return fmt.Errorf("fonts: materialize %s: %w", item.name, err)
		}
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("fonts: sync destination: %w", err)
	}
	return nil
}

func openPrivateDirectory(dest string) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, fmt.Errorf("fonts: open filesystem root: %w", err)
	}
	parts := strings.Split(strings.TrimPrefix(dest, string(filepath.Separator)), string(filepath.Separator))
	for index, part := range parts {
		next, err := unix.Openat(fd, part, flags, 0)
		if errors.Is(err, unix.ENOENT) && index == len(parts)-1 {
			if err = unix.Mkdirat(fd, part, 0700); err == nil {
				next, err = unix.Openat(fd, part, flags, 0)
			}
		}
		unix.Close(fd)
		if err != nil {
			return nil, fmt.Errorf("fonts: open destination component %s: %w", part, err)
		}
		fd = next
	}
	var state unix.Stat_t
	if err := unix.Fstat(fd, &state); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("fonts: inspect destination: %w", err)
	}
	if state.Mode&unix.S_IFMT != unix.S_IFDIR || state.Mode&07777 != 0700 || state.Uid != uint32(os.Geteuid()) {
		unix.Close(fd)
		return nil, fmt.Errorf("fonts: destination must be an owned private directory with permissions 0700")
	}
	return os.NewFile(uintptr(fd), dest), nil
}

func installAsset(directory *os.File, item asset, data []byte) error {
	fd := int(directory.Fd())
	existing, err := openAsset(fd, item.name)
	if err == nil {
		inspection := verifyAsset(existing, data, item.digest)
		closeErr := existing.Close()
		if inspection != nil {
			return inspection
		}
		return closeErr
	}
	if !errors.Is(err, unix.ENOENT) {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("generate temporary name: %w", err)
	}
	temporary := ".font-" + hex.EncodeToString(random[:])
	tempFD, err := unix.Openat(fd, temporary, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return fmt.Errorf("create temporary asset: %w", err)
	}
	file := os.NewFile(uintptr(tempFD), temporary)
	defer file.Close()
	defer unix.Unlinkat(fd, temporary, 0)
	if count, err := file.Write(data); err != nil {
		return fmt.Errorf("write asset: %w", err)
	} else if count != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync asset: %w", err)
	}
	if err := verifyAsset(file, data, item.digest); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Linkat(fd, temporary, fd, item.name, 0); err != nil {
		return fmt.Errorf("publish asset without replacement: %w", err)
	}
	if err := unix.Unlinkat(fd, temporary, 0); err != nil {
		return fmt.Errorf("remove temporary asset: %w", err)
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync published asset: %w", err)
	}
	published, err := openAsset(fd, item.name)
	if err != nil {
		return fmt.Errorf("read back published asset: %w", err)
	}
	inspection := verifyAsset(published, data, item.digest)
	closeErr := published.Close()
	if inspection != nil {
		return inspection
	}
	return closeErr
}

func openAsset(directory int, name string) (*os.File, error) {
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func verifyAsset(file *os.File, expected []byte, expectedDigest string) error {
	var state unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &state); err != nil {
		return fmt.Errorf("inspect asset: %w", err)
	}
	if state.Mode&unix.S_IFMT != unix.S_IFREG || state.Mode&07777 != 0600 || state.Uid != uint32(os.Geteuid()) || state.Nlink != 1 || state.Size != int64(len(expected)) {
		return fmt.Errorf("asset must be an owned regular private file with one link and the expected size")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind asset: %w", err)
	}
	actual, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	if err != nil {
		return fmt.Errorf("read back asset: %w", err)
	}
	if !bytes.Equal(actual, expected) || digest(actual) != expectedDigest {
		return fmt.Errorf("asset readback differs from embedded bytes or digest")
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

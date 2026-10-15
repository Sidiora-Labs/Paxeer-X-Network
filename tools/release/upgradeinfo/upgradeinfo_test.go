package upgradeinfo_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	appparams "github.com/Sidiora-Labs/Paxeer-X-Network/node/params"
	store "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"
)

const releaseName = "v6.12.0"
const releaseTag = "paxeer-network/" + releaseName
const releaseAsset = "paxd-" + releaseName + "-linux-amd64"
const releaseURL = "https://github.com/Sidiora-Labs/Paxeer-X-Network/releases/download/" + releaseTag + "/" + releaseAsset

func checksumOf(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func runUpgradeInfo(t *testing.T, wantExit int, args ...string) string {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "upgrade_info.py"))
	require.NoError(t, err)
	command := exec.Command("python3", append([]string{script}, args...)...)
	command.Dir = t.TempDir()
	output, err := command.CombinedOutput()
	if wantExit == 0 {
		require.NoError(t, err, "%s", output)
	} else {
		var exitError *exec.ExitError
		require.True(t, errors.As(err, &exitError), "error: %v; output: %s", err, output)
		require.Equal(t, wantExit, exitError.ExitCode(), "%s", output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return contents
}

func assertNodeFile(t *testing.T, home string, height int64) {
	t.Helper()
	path := filepath.Join(home, "data", upgradekeeper.UpgradeInfoFileName)
	contents := readFile(t, path)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(contents, &fields))
	require.Len(t, fields, 2)
	require.JSONEq(t, `{"name":"`+releaseName+`","height":`+strconv.FormatInt(height, 10)+`}`, string(contents))
	stat, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), stat.Mode().Perm())
	keeper := upgradekeeper.NewKeeper(map[int64]bool{}, sdk.NewKVStoreKey(upgradetypes.StoreKey), appparams.MakeEncodingConfig().Marshaler, home, nil)
	info, err := keeper.ReadUpgradeInfoFromDisk()
	require.NoError(t, err)
	require.Equal(t, store.UpgradeInfo{Name: releaseName, Height: height}, info)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, upgradekeeper.UpgradeInfoFileName, entries[0].Name())
}

func releaseDocument(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	sums := filepath.Join(directory, "SHA256SUMS")
	checksum := checksumOf("paxd release artifact")
	writeFile(t, sums, checksum+"  "+releaseAsset+"\n")
	document := filepath.Join(directory, "upgrade-info.json")
	require.Empty(t, runUpgradeInfo(t, 0, "release", "--sums", sums, "--tag", releaseTag, "--out", document))
	return document, checksum
}

func TestUpgradeInfoReadByKeeper(t *testing.T) {
	home := t.TempDir()
	require.Empty(t, runUpgradeInfo(t, 0, "node", "--home", home, "--height", "1001"))
	assertNodeFile(t, home, 1001)
	path := filepath.Join(home, "data", upgradekeeper.UpgradeInfoFileName)
	original := readFile(t, path)
	oldFile, err := os.Open(path)
	require.NoError(t, err)
	defer oldFile.Close()
	require.NoError(t, os.Chmod(path, 0o644))
	require.Empty(t, runUpgradeInfo(t, 0, "node", "--home", home, "--halt-height", "1001"))
	assertNodeFile(t, home, 1002)
	oldContents := make([]byte, len(original))
	n, err := oldFile.Read(oldContents)
	require.NoError(t, err)
	require.Equal(t, len(original), n)
	require.Equal(t, original, oldContents)
}

func TestReleaseDocumentNamesChecksum(t *testing.T) {
	checksum := checksumOf("paxd release artifact")
	for _, marker := range []string{" ", "*"} {
		t.Run("marker_"+strconv.Itoa(int(marker[0])), func(t *testing.T) {
			directory := t.TempDir()
			sums := filepath.Join(directory, "SHA256SUMS")
			writeFile(t, sums, checksumOf("sidecar")+"  price-sidecar-"+releaseName+"-linux-amd64\n"+checksum+" "+marker+releaseAsset+"\n")
			output := runUpgradeInfo(t, 0, "release", "--sums", sums, "--tag", releaseTag)
			var document map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(output), &document))
			require.Len(t, document, 2)
			require.JSONEq(t, `"`+releaseName+`"`, string(document["name"]))
			var info string
			require.NoError(t, json.Unmarshal(document["info"], &info))
			require.JSONEq(t, `{"binaries":{"linux/amd64":"`+releaseURL+`?checksum=sha256:`+checksum+`"}}`, info)
			path := filepath.Join(directory, "upgrade-info.json")
			require.Empty(t, runUpgradeInfo(t, 0, "release", "--sums", sums, "--out", path))
			require.Equal(t, output, string(readFile(t, path)))
			home := t.TempDir()
			require.Empty(t, runUpgradeInfo(t, 0, "node", "--home", home, "--halt-height", "1000", "--release-doc", path, "--sha256", checksum))
			assertNodeFile(t, home, 1001)
			original := readFile(t, filepath.Join(home, "data", upgradekeeper.UpgradeInfoFileName))
			refusal := runUpgradeInfo(t, 1, "node", "--home", home, "--halt-height", "1001", "--release-doc", path, "--sha256", checksumOf("different artifact"))
			require.Equal(t, "upgrade_info: release document checksum differs\n", refusal)
			require.Equal(t, original, readFile(t, filepath.Join(home, "data", upgradekeeper.UpgradeInfoFileName)))
		})
	}
	t.Run("refuse_invalid_documents", func(t *testing.T) {
		path, checksum := releaseDocument(t)
		original := string(readFile(t, path))
		cases := []struct{ name, document, message string }{
			{"wrong_name", strings.Replace(original, `"name": "`+releaseName+`"`, `"name": "v6.11"`, 1), "name v6.11 is not the release upgrade " + releaseName},
			{"array", `[]`, "release document must contain name and info"},
			{"missing_info", `{"name":"` + releaseName + `"}`, "release document must contain name and info"},
			{"object_info", `{"name":"` + releaseName + `","info":{}}`, "release document info must be a JSON string"},
			{"missing_binaries", `{"name":"` + releaseName + `","info":"{}"}`, "release document must contain binaries"},
			{"wrong_asset", strings.Replace(original, releaseAsset, "layerxd-"+releaseName+"-linux-amd64", 1), "release document must name the release paxd asset"},
			{"bad_checksum", strings.Replace(original, checksum, strings.ToUpper(checksum), 1), "checksum must be 64 lowercase hex digits"},
			{"trailing_query", strings.Replace(original, checksum, checksum+"&extra=value", 1), "checksum must be 64 lowercase hex digits"},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				document := filepath.Join(t.TempDir(), "upgrade-info.json")
				writeFile(t, document, test.document)
				home := t.TempDir()
				output := runUpgradeInfo(t, 1, "node", "--home", home, "--height", "1001", "--release-doc", document, "--sha256", checksum)
				require.Equal(t, "upgrade_info: "+test.message+"\n", output)
				_, err := os.Stat(filepath.Join(home, "data"))
				require.ErrorIs(t, err, os.ErrNotExist)
			})
		}
	})
}

func TestReleaseDocumentRequiresPaxdLine(t *testing.T) {
	checksum := checksumOf("paxd release artifact")
	cases := []struct{ name, sums, message string }{
		{"empty", "", "no " + releaseAsset + " line in SHA256SUMS"},
		{"sidecar_only", checksum + "  price-sidecar-" + releaseName + "-linux-amd64\n", "no " + releaseAsset + " line in SHA256SUMS"},
		{"wrong_version", checksum + "  paxd-v6.11-linux-amd64\n", "no " + releaseAsset + " line in SHA256SUMS"},
		{"wrong_arch", checksum + "  paxd-" + releaseName + "-linux-arm64\n", "no " + releaseAsset + " line in SHA256SUMS"},
		{"suffix", checksum + "  " + releaseAsset + ".backup\n", "no " + releaseAsset + " line in SHA256SUMS"},
		{"duplicate", strings.Repeat(checksum+"  "+releaseAsset+"\n", 2), "multiple " + releaseAsset + " lines in SHA256SUMS"},
		{"conflicting", checksum + "  " + releaseAsset + "\n" + checksumOf("different artifact") + "  " + releaseAsset + "\n", "multiple " + releaseAsset + " lines in SHA256SUMS"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			sums := filepath.Join(directory, "SHA256SUMS")
			writeFile(t, sums, test.sums)
			output := filepath.Join(directory, "upgrade-info.json")
			writeFile(t, output, "existing output")
			result := runUpgradeInfo(t, 1, "release", "--sums", sums, "--out", output)
			require.Equal(t, "upgrade_info: "+test.message+"\n", result)
			require.Equal(t, "existing output", string(readFile(t, output)))
		})
	}
}

func TestNodeFileHeightIsHaltPlusOne(t *testing.T) {
	for _, halt := range []int64{1, 1000, (1 << 63) - 2} {
		t.Run(strconv.FormatInt(halt, 10), func(t *testing.T) {
			home := t.TempDir()
			require.Empty(t, runUpgradeInfo(t, 0, "node", "--home", home, "--halt-height", strconv.FormatInt(halt, 10)))
			assertNodeFile(t, home, halt+1)
			require.Empty(t, runUpgradeInfo(t, 0, "node", "--home", home, "--halt-height", strconv.FormatInt(halt, 10), "--height", strconv.FormatInt(halt+1, 10)))
			assertNodeFile(t, home, halt+1)
		})
	}
	home := t.TempDir()
	require.Equal(t, "upgrade_info: height must be halt height + 1\n", runUpgradeInfo(t, 1, "node", "--home", home, "--halt-height", "1000", "--height", "1000"))
	_, err := os.Stat(filepath.Join(home, "data"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRefusesWrongNameAndHeight(t *testing.T) {
	document, checksum := releaseDocument(t)
	cases := []struct {
		name    string
		args    []string
		message string
	}{
		{"wrong_name", []string{"--name", "v6.11", "--height", "1001"}, "name v6.11 is not the release upgrade " + releaseName},
		{"zero_height", []string{"--height", "0"}, "height must be positive"},
		{"negative_height", []string{"--height", "-1"}, "height must be positive"},
		{"zero_halt", []string{"--halt-height", "0"}, "halt height must be positive"},
		{"negative_halt", []string{"--halt-height", "-1"}, "halt height must be positive"},
		{"overflow_height", []string{"--height", "9223372036854775808"}, "height exceeds int64"},
		{"overflow_halt", []string{"--halt-height", "9223372036854775808"}, "halt height exceeds int64"},
		{"overflow_next_block", []string{"--halt-height", "9223372036854775807"}, "height exceeds int64"},
		{"no_height", nil, "--height or --halt-height is required"},
		{"missing_checksum", []string{"--height", "1001", "--release-doc", document}, "--release-doc and --sha256 must be supplied together"},
		{"missing_document", []string{"--height", "1001", "--sha256", checksum}, "--release-doc and --sha256 must be supplied together"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			output := runUpgradeInfo(t, 1, append([]string{"node", "--home", home}, test.args...)...)
			require.Equal(t, "upgrade_info: "+test.message+"\n", output)
			_, err := os.Stat(filepath.Join(home, "data"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
	for _, invalid := range []string{"", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		t.Run("checksum_"+invalid, func(t *testing.T) {
			directory := t.TempDir()
			sums := filepath.Join(directory, "SHA256SUMS")
			writeFile(t, sums, invalid+"  "+releaseAsset+"\n")
			expected := "upgrade_info: checksum must be 64 lowercase hex digits\n"
			if invalid == "" {
				expected = "upgrade_info: no " + releaseAsset + " line in SHA256SUMS\n"
			}
			require.Equal(t, expected, runUpgradeInfo(t, 1, "release", "--sums", sums))
			require.Equal(t, "upgrade_info: checksum must be 64 lowercase hex digits\n", runUpgradeInfo(t, 1, "node", "--home", directory, "--height", "1001", "--release-doc", document, "--sha256", invalid))
			_, err := os.Stat(filepath.Join(directory, "data"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
	sums := filepath.Join(t.TempDir(), "SHA256SUMS")
	writeFile(t, sums, checksum+"  "+releaseAsset+"\n")
	require.Equal(t, "upgrade_info: name v6.11 is not the release upgrade "+releaseName+"\n", runUpgradeInfo(t, 1, "release", "--sums", sums, "--name", "v6.11"))
	require.Equal(t, "upgrade_info: tag paxeer-network/v6.11 is not the release tag "+releaseTag+"\n", runUpgradeInfo(t, 1, "release", "--sums", sums, "--tag", "paxeer-network/v6.11"))
}

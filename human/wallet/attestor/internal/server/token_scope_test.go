package server

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/audit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/lxwire"
)

func replayAudits(t *testing.T, c *testCluster, node *testNode) int {
	t.Helper()
	logged, err := os.ReadFile(filepath.Join(c.configs[node.index].nodeDir, "audit", audit.FileName))
	if err != nil {
		t.Fatalf("%s: read audit log: %v", node.id, err)
	}
	return bytes.Count(logged, []byte(auditReasonTokenReplayed))
}

func expectReplay(t *testing.T, label string, r apiResult) {
	t.Helper()
	e := expectError(t, label, r, CodeTokenInvalid)
	if !strings.Contains(e.Message, "already authorised") {
		t.Fatalf("%s: %s", label, e.Message)
	}
}

func TestOneTokenAuthorisesDistinctRequestsAndRefusesARepeat(t *testing.T) {
	c := newTestCluster(t, 5, true)
	pub, account := importEd25519(t, c, "lx-key", "attestor request-scoped token ed25519 key")
	signers := []string{"node-1", "node-2", "node-3", "node-4", "node-5"}
	token := c.idp.mint(t, c.idp.key, testOwner)

	verifyBind(t, "first request under the token", pub, account, signBind(t, c, "lx-key", "token-scope-first", signers, account, token))
	verifyBind(t, "second request under the same token", pub, account, signBind(t, c, "lx-key", "token-scope-second", signers, account, token))

	for i, r := range signBind(t, c, "lx-key", "token-scope-first", signers, account, token) {
		expectReplay(t, "the first request repeated on "+signers[i], r)
	}
	for _, node := range c.nodes {
		if n := replayAudits(t, c, node); n != 1 {
			t.Fatalf("%s: %d audit entries name the replay, want 1", node.id, n)
		}
	}

	restarted := c.restart(t, c.byID("node-3")[0])
	bind := lxwire.BindMessage(testChainID, common.HexToAddress(account), 1)
	for _, session := range []string{"token-scope-first", "token-scope-second"} {
		r := c.call(t, restarted, PathSign, SignRequest{SessionID: session, KeyID: "lx-key", Kind: KindLXBind, Signers: signers, Message: hex.EncodeToString(bind)}, token)
		expectReplay(t, session+" repeated after the node restarted", r)
	}
	if n := replayAudits(t, c, restarted); n != 3 {
		t.Fatalf("%s: %d audit entries name the replay after restart, want 3", restarted.id, n)
	}

	verifyBind(t, "a third request under the same token after a restart", pub, account, signBind(t, c, "lx-key", "token-scope-third", signers, account, token))
}

#!/usr/bin/env python3
import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import zlib

import yaml

ROOT = Path(__file__).resolve().parents[3]
PUBLISHER = ROOT / "platform/hosted/tests/publish-images.sh"
WORKFLOW = ROOT / ".github/workflows/publish-images.yml"
FIXTURE_SOURCE = "https://github.com/cli/cli/tree/49f72234acd346666a4646a0f5a427fb3543debc/pkg/cmd/attestation/test/data"
SOURCE = "95baf27389e83e6a5c48f42e190d48d7abcea19e"
REPOSITORY = "malancas/attest-demo"
SIGNER = "github/artifact-attestations-workflows/.github/workflows/attest.yml"
SUBJECT = "github_provenance_demo-0.0.0-py3-none-any.whl"
DIGEST = "sha256:49a3aa6075e0f49f82843e74b5baa614ad2a588e6675612bf108a0a008c5ac25"
FIXTURES = {'reusable-workflow-artifact': {'data': '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',
                                'sha256': '49a3aa6075e0f49f82843e74b5baa614ad2a588e6675612bf108a0a008c5ac25'},
 'reusable-workflow-attestation.sigstore.json': {'data': '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',
                                                 'sha256': '88e35c3496fc9b9bd29629b69271bd32738e170f0d85a12d67da0c72c743f3bd'},
 'trusted_root.json': {'data': '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',
                       'sha256': '455b3fe53e2678889f093d66ebbfda5f12ebb9d682b146c3535fa04e97689787'}}


class CandidateProvenance(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="candidate-provenance-")
        cls.addClassCleanup(cls.temp.cleanup)
        cls.directory = Path(cls.temp.name)
        cls.env = {
            "PATH": os.defpath,
            "LC_ALL": "C",
            "GH_CONFIG_DIR": str(cls.directory / "gh-config"),
            "GH_CACHE_DIR": str(cls.directory / "gh-cache"),
            "GH_HOST": "github.com",
            "GH_PROMPT_DISABLED": "1",
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": "/dev/null",
        }
        for name, fixture in FIXTURES.items():
            data = zlib.decompress(base64.b64decode(fixture["data"]))
            if hashlib.sha256(data).hexdigest() != fixture["sha256"]:
                raise AssertionError(f"changed upstream signed fixture: {name}")
            (cls.directory / name).write_bytes(data)
        cls.repo = cls.directory / "repository"
        cls.repo.mkdir()
        cls.git("init", "--initial-branch=main")
        cls.git("config", "user.name", "Candidate provenance test")
        cls.git("config", "user.email", "candidate-test@example.invalid")
        (cls.repo / "source.txt").write_text("first immutable candidate\n")
        cls.git("add", "source.txt")
        cls.git("commit", "-m", "First candidate")
        cls.older = cls.git("rev-parse", "HEAD")
        (cls.repo / "source.txt").write_text("next immutable candidate\n")
        cls.git("commit", "-am", "Next candidate")
        cls.newer = cls.git("rev-parse", "HEAD")
        cls.git("tag", "sdk-v-source-policy")

    @classmethod
    def git(cls, *args):
        result = subprocess.run(
            ["git", "-C", str(cls.repo), *args], env=cls.env,
            text=True, capture_output=True, timeout=15, check=True,
        )
        return result.stdout.strip()

    def execute(self, args, **env):
        return subprocess.run(
            args, env={**self.env, **env}, cwd=self.directory,
            text=True, capture_output=True, timeout=30,
        )

    def shell(self, script, *args, **env):
        return self.execute(
            ["bash", "-c", 'source "$1"; shift; ' + script,
             "candidate-provenance", str(PUBLISHER), *map(str, args)], **env,
        )

    def policy(self, candidate, event):
        return self.execute(
            ["bash", str(PUBLISHER), "--source-policy"],
            LAYERX_PUBLISH_REVISION=candidate, GITHUB_SHA=event,
        )

    def assert_refused(self, result, reason):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(reason, result.stderr)

    def test_matching_event_candidate_is_accepted(self):
        result = self.policy(self.newer, self.newer)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(self.newer, result.stdout)

    def test_earlier_default_branch_candidate_is_refused(self):
        self.git("merge-base", "--is-ancestor", self.older, self.newer)
        self.assert_refused(self.policy(self.older, self.newer), "differs from workflow event")

    def test_different_event_and_missing_or_abbreviated_sha_are_refused(self):
        for candidate, event, reason in [
            (self.newer, self.older, "differs from workflow event"),
            ("", self.newer, "candidate must be"),
            (self.newer[:12], self.newer, "candidate must be"),
            (self.newer, "", "GITHUB_SHA must be"),
            (self.newer, self.newer[:12], "GITHUB_SHA must be"),
        ]:
            with self.subTest(candidate=candidate, event=event):
                self.assert_refused(self.policy(candidate, event), reason)

    def test_tag_binding_uses_the_same_event_policy(self):
        script = (
            'REPO_ROOT=$1; RELEASE_TAG_NAME=sdk-v-source-policy; '
            'BUILD_REVISION=$2; PUBLISH_TAG=$2; '
            'resolve_release_binding; require_gated_publication'
        )
        env = dict(GITHUB_ACTIONS="true", GITHUB_REPOSITORY="example/repository",
                   LAYERX_PUBLISH_GATE_REVISION=self.newer, GITHUB_SHA=self.newer)
        result = self.shell(script, self.repo, self.newer, **env)
        self.assertEqual(result.returncode, 0, result.stderr)
        env["GITHUB_SHA"] = self.older
        self.assert_refused(self.shell(script, self.repo, self.newer, **env),
                            "differs from workflow event")

    def test_candidate_is_refused_before_default_branch_network_lookup(self):
        result = self.shell(
            'RELEASE_COMMIT=$1; RELEASE_KIND=candidate; BUILD_REVISION=$1; '
            'PUBLISH_TAG=$1; require_gated_publication', self.older,
            GITHUB_ACTIONS="true", GITHUB_REPOSITORY="example/repository",
            LAYERX_PUBLISH_GATE_REVISION=self.older, GITHUB_SHA=self.newer,
        )
        self.assert_refused(result, "differs from workflow event")

    def verify(self, *, source=SOURCE, repository=REPOSITORY, signer=SIGNER,
               subject=SUBJECT, digest=DIGEST, artifact=None, bundle=None):
        output = self.directory / (self.id().split(".")[-1] + ".verified.json")
        result = self.shell(
            'verify_provenance "$@"',
            artifact or self.directory / "reusable-workflow-artifact",
            subject, digest, repository, signer, source, output,
            bundle or self.directory / "reusable-workflow-attestation.sigstore.json",
            self.directory / "trusted_root.json",
        )
        if result.returncode:
            self.assertFalse(output.exists(), "failed verification left credited provenance")
        return result, output

    def test_real_signed_artifact_matches_source_signer_and_digest(self):
        result, output = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        verified = json.loads(output.read_text())
        self.assertTrue(verified)
        self.assertEqual(verified[0]["verificationResult"]["statement"]["subject"][0],
                         {"name": SUBJECT, "digest": {"sha256": DIGEST[7:]}})

    def test_signed_attestation_with_wrong_source_is_refused(self):
        result, _ = self.verify(source=self.older)
        self.assert_refused(result, "no build provenance")

    def test_signed_attestation_with_wrong_source_repository_is_refused(self):
        result, _ = self.verify(repository="example/wrong-source")
        self.assert_refused(result, "no build provenance")

    def test_signed_attestation_with_wrong_signer_is_refused(self):
        result, _ = self.verify(signer="example/wrong/.github/workflows/publish.yml")
        self.assert_refused(result, "no build provenance")

    def test_valid_signature_with_wrong_image_name_is_refused(self):
        result, _ = self.verify(subject="ghcr.io/example/wrong-image")
        self.assert_refused(result, "does not name the published subject and digest")

    def test_valid_signature_with_wrong_image_digest_is_refused(self):
        result, _ = self.verify(digest="sha256:" + "0" * 64)
        self.assert_refused(result, "does not name the published subject and digest")

    def test_modified_artifact_is_refused(self):
        artifact = self.directory / "modified-artifact"
        artifact.write_bytes((self.directory / "reusable-workflow-artifact").read_bytes() + b"changed")
        result, _ = self.verify(artifact=artifact)
        self.assert_refused(result, "no build provenance")

    def test_modified_signature_is_refused(self):
        data = json.loads((self.directory / "reusable-workflow-attestation.sigstore.json").read_text())
        signature = bytearray(base64.b64decode(data["dsseEnvelope"]["signatures"][0]["sig"]))
        signature[-1] ^= 1
        data["dsseEnvelope"]["signatures"][0]["sig"] = base64.b64encode(signature).decode()
        bundle = self.directory / "modified-signature.json"
        bundle.write_text(json.dumps(data))
        result, _ = self.verify(bundle=bundle)
        self.assert_refused(result, "no build provenance")

    def test_failed_reverification_removes_prior_credit(self):
        result, output = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(output.exists())
        result, _ = self.verify(source=self.older)
        self.assert_refused(result, "no build provenance")
        self.assertFalse(output.exists())

    def test_malformed_source_removes_prior_credit(self):
        result, output = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        result, _ = self.verify(source=SOURCE[:12])
        self.assert_refused(result, "invalid provenance source commit")
        self.assertFalse(output.exists())

    def test_missing_bundle_is_refused(self):
        result, _ = self.verify(bundle=self.directory / "missing-bundle.json")
        self.assert_refused(result, "offline provenance requires a bundle and trusted root")

    def test_registry_verification_cannot_override_trust_root(self):
        result, _ = self.verify(artifact="oci://ghcr.io/example/image@" + DIGEST)
        self.assert_refused(result, "registry provenance uses the standard trust roots")

    def test_workflow_binds_event_before_build_and_preserves_promotion_gates(self):
        workflow = yaml.load(WORKFLOW.read_text(), Loader=yaml.BaseLoader)
        jobs = workflow["jobs"]
        steps = jobs["gate"]["steps"]
        self.assertEqual(steps[0]["with"]["ref"], "${{ github.sha }}")
        self.assertIn("publish-images.sh --source-policy", steps[1]["run"])
        self.assertIn('test "$revision" = "$LAYERX_PUBLISH_REVISION"', steps[1]["run"])
        self.assertEqual(jobs["publish"]["needs"], ["gate"])
        self.assertEqual(jobs["attest"]["needs"], ["publish"])
        self.assertEqual(jobs["promote"]["needs"], ["gate", "publish", "attest"])
        action = next(s for s in jobs["attest"]["steps"] if "attest-build-provenance@" in s.get("uses", ""))
        self.assertEqual(action["uses"], "actions/attest-build-provenance@4d101475d8b20a2381f78447822ac1eab6504dd8")
        self.assertEqual(action["with"]["subject-digest"],
                         "${{ fromJSON(needs.publish.outputs.published)[matrix.image].digest }}")
        publisher = PUBLISHER.read_text()
        promote = publisher.split("mode_promote() {", 1)[1].split("\n}", 1)[0]
        self.assertLess(promote.index("require_gated_publication"), promote.index("verify_published"))
        self.assertLess(promote.index("verify_published"), promote.index("promote_tags"))
        push = publisher.split("mode_push() {", 1)[1].split("\n}", 1)[0]
        self.assertLess(push.index("require_gated_publication"), push.index("push_images"))
        verification = publisher.split("verify_published() {", 1)[1].split("\n}", 1)[0]
        checks = ['registry_image_digest "$target:$PUBLISH_TAG"', "cosign verify ",
                  "cosign verify-attestation", '[ "$attested" = "$built" ]',
                  'verify_provenance "oci://$target@$digest"']
        positions = [verification.index(check) for check in checks]
        self.assertEqual(positions, sorted(positions))
        self.assertIn('"$GITHUB_REPOSITORY" "$GITHUB_REPOSITORY/$PUBLISH_WORKFLOW" "$RELEASE_COMMIT"', verification)


if __name__ == "__main__":
    unittest.main(verbosity=2)

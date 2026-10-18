"""Legitimate protocol coverage source remains bound, outputs/secrets do not."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from dispatcher.source_policy import SOURCE_POLICY_VERSION, ignored
from dispatcher.evidence import source_digest, _paths, EvidenceError
from dispatcher.patches import build_patch, stage_patches
from dispatcher.workspace import create_archive, create_workspace

class SourcePolicyTests(unittest.TestCase):
    def test_exact_protocol_source_exception_and_nested_outputs(self):
        for path in ('protocol/coverage','protocol/coverage/checkpoint.go','protocol/coverage/fixtures/canonical.json'):
            self.assertFalse(ignored(path),path)
        for path in ('coverage/index.html','client/coverage/index.html','protocol/coverage/coverage/index.html','protocol/coverage/dist/generated.go','protocol/coverage/node_modules/pkg/a.js','protocol/coverage/.env','protocol/coverage/secrets/key.json','protocol/coverage/coverage.out','other/protocol/coverage/check.go'):
            self.assertTrue(ignored(path),path)
        self.assertEqual(SOURCE_POLICY_VERSION,'qualification-source-v1')

    def test_real_source_hash_archive_patch_and_declared_input(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);base=root/'base';base.mkdir();(base/'anchor.txt').write_text('baseline')
            candidate=root/'candidate';candidate.mkdir();(candidate/'anchor.txt').write_text('baseline')
            source=candidate/'protocol/coverage';source.mkdir(parents=True);(source/'checkpoint.go').write_text('package coverage\n');(source/'fixtures').mkdir();(source/'fixtures/receipt.json').write_text('{"height":"125"}\n')
            before=source_digest(candidate);self.assertNotEqual(source_digest(base),before)
            declared=_paths(['protocol/coverage/checkpoint.go','protocol/coverage/fixtures/receipt.json'],candidate);self.assertEqual(len(declared),2)
            (source/'checkpoint.go').write_text('package coverage\n// changed\n');self.assertNotEqual(source_digest(candidate),before)
            (source/'node_modules').symlink_to(root/'missing-dependencies',target_is_directory=True);(source/'.env').symlink_to(root/'missing-secret');(source/'dist').mkdir();(source/'dist/evil').symlink_to('/etc/passwd');(candidate/'coverage').mkdir();(candidate/'coverage/evil').symlink_to('/etc/passwd')
            bound=source_digest(candidate);(source/'dist/generated.go').write_text('ignored generated output');self.assertEqual(source_digest(candidate),bound)
            with self.assertRaises(EvidenceError):_paths(['protocol/coverage/dist/generated.go'],candidate)
            archive=root/'candidate.tar';create_archive(candidate,archive);extracted=root/'extract';create_workspace(archive,extracted);self.assertTrue((extracted/'protocol/coverage/checkpoint.go').exists());self.assertTrue((extracted/'protocol/coverage/fixtures/receipt.json').exists());self.assertFalse((extracted/'protocol/coverage/dist').exists());self.assertFalse((extracted/'protocol/coverage/.env').exists())
            patch=build_patch(base,candidate,['protocol/coverage/**'],root/'patch');self.assertEqual({x['path'] for x in patch['entries']},{'protocol/coverage/checkpoint.go','protocol/coverage/fixtures/receipt.json'})
            staged=stage_patches(base,[patch],root/'stage',verify=lambda p:source_digest(p)==bound,ownership={patch['patch_digest']:['protocol/coverage/**']});self.assertTrue(staged['qualified'])
            self.assertEqual(source_digest(root/'stage'),bound)
            self.assertFalse((root/'stage/coverage').exists())

    def test_shared_secrets_go_only_source_boundary(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);source=root/'shared/secrets';source.mkdir(parents=True)
            (source/'manager.go').write_text('package secrets\n')
            (source/'manager_test.go').write_text('package secrets\n// tests\n')
            for name in ('credentials.json','private.pem','.env','key.txt'):
                (source/name).symlink_to(root/'unread-secret-target')
            (source/'generated').mkdir();(source/'generated/secret.go').symlink_to('/etc/passwd')
            before=source_digest(root)
            self.assertEqual(len(_paths(['shared/secrets/manager.go','shared/secrets/manager_test.go'],root)),2)
            for name in ('credentials.json','private.pem','.env','key.txt','generated/secret.go'):
                self.assertTrue(ignored('shared/secrets/'+name))
                with self.assertRaises(EvidenceError):_paths(['shared/secrets/'+name],root)
            (source/'manager.go').write_text('package secrets\n// actual change\n')
            self.assertNotEqual(before,source_digest(root))
            archive=root.parent/(root.name+'-source.tar')
            self.addCleanup(archive.unlink,missing_ok=True)
            create_archive(root,archive);extracted=root.parent/(root.name+'-extract')
            import shutil;self.addCleanup(shutil.rmtree,extracted,ignore_errors=True)
            create_workspace(archive,extracted)
            self.assertEqual({p.name for p in (extracted/'shared/secrets').iterdir()},{'manager.go','manager_test.go'})
            (source/'evil.go').symlink_to('/etc/passwd')
            with self.assertRaises(EvidenceError):source_digest(root)
            (source/'evil.go').unlink()
            os.link(source/'manager.go',source/'alias.go')
            with self.assertRaises(EvidenceError):source_digest(root)

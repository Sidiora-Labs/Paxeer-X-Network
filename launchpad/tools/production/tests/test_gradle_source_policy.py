import importlib.util,json,pathlib,subprocess,sys,tarfile,tempfile,unittest
PRODUCTION=pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0,str(PRODUCTION))
from dispatcher import evidence,source_policy,workspace
loader=importlib.util.spec_from_file_location('candidate_qualify',PRODUCTION/'qualify.py');qualify=importlib.util.module_from_spec(loader);loader.loader.exec_module(qualify)

class GradleSourcePolicy(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=pathlib.Path(self.tmp.name)/'source';self.root.mkdir()
  self.sources=['client/apps/mobile/android/gradle/wrapper/gradle-wrapper.jar','client/apps/mobile/android/gradle/wrapper/gradle-wrapper.properties','client/apps/mobile/android/build.gradle','client/apps/mobile/android/gradle.properties','client/apps/mobile/android/app/src/main/AndroidManifest.xml','client/apps/mobile/android/app/src/main/java/app/MainActivity.kt','client/apps/mobile/android/.gradle-backup/keep.txt']
  for name in self.sources:
   path=self.root/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(b'actual tracked source: '+name.encode())
 def test_exact_basename_exclusion_only(self):
  for name in ['.gradle/file','android/.gradle/8.13/gc.properties','client/apps/mobile/android/.gradle/buildOutputCleanup/buildOutputCleanup.lock']:
   with self.subTest(name=name):self.assertTrue(source_policy.ignored(name));self.assertTrue(qualify.ignored_source_directory(name))
  for name in self.sources:
   with self.subTest(name=name):self.assertFalse(source_policy.ignored(name));self.assertFalse(qualify.ignored_source_directory(name))
 def test_cache_bytes_and_locks_do_not_drift_either_snapshot(self):
  before=evidence.source_digest(self.root);old=qualify.snapshot(self.root)
  for name in ['android/.gradle/locks/cache.lock','client/apps/mobile/android/.gradle/8.13/gc.bin']:
   path=self.root/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(bytes(range(256)))
  self.assertEqual(before,evidence.source_digest(self.root));self.assertEqual(old,qualify.snapshot(self.root))
  self.assertEqual(set(old['files']),set(self.sources))
 def test_native_source_and_gradle_wrapper_drift_still_detected(self):
  for name in self.sources:
   with self.subTest(name=name):
    path=self.root/name;before=evidence.source_digest(self.root);snapshot=qualify.snapshot(self.root);original=path.read_bytes();path.write_bytes(original+b' modified')
    self.assertNotEqual(before,evidence.source_digest(self.root));self.assertNotEqual(snapshot,qualify.snapshot(self.root));path.write_bytes(original)
 def test_ignored_cache_symlink_is_not_followed(self):
  before=evidence.source_digest(self.root);old=qualify.snapshot(self.root)
  (self.root/'client/apps/mobile/android/.gradle').symlink_to('/nonexistent-generated-build-cache',target_is_directory=True)
  self.assertEqual(before,evidence.source_digest(self.root));self.assertEqual(old,qualify.snapshot(self.root))
 def test_cache_cannot_be_declared_as_qualified_input(self):
  path=self.root/'client/apps/mobile/android/.gradle/lock';path.parent.mkdir(parents=True);path.write_text('lock')
  with self.assertRaises(evidence.EvidenceError):evidence._paths([path.relative_to(self.root).as_posix()],self.root)
 def test_archive_retains_wrapper_and_native_source_only(self):
  path=self.root/'client/apps/mobile/android/.gradle/gc.bin';path.parent.mkdir(parents=True);path.write_bytes(b'generated')
  archive=pathlib.Path(self.tmp.name)/'archive.tar';workspace.create_archive(self.root,archive)
  with tarfile.open(archive) as stream:files={m.name for m in stream if m.isfile()}
  self.assertEqual(files,set(self.sources))

if __name__=='__main__':unittest.main()

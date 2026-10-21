import shutil
import tempfile
from pathlib import Path
import unittest
from dispatcher.patches import build_patch, stage_patches, PatchError, _reserved

class OwnedLeafArtifactTests(unittest.TestCase):
 def test_exact_owned_leaf_artifacts_patch_and_stage(self):
  for lane,paths in [('client/packages/sdk/**',['client/packages/sdk/package.json','client/packages/sdk/src/types.gen.ts']),('client/apps/mobile/**',['client/apps/mobile/package.json'])]:
   with self.subTest(lane=lane),tempfile.TemporaryDirectory() as tmp:
    root=Path(tmp);base=root/'base';base.mkdir();candidate=root/'candidate';candidate.mkdir()
    for path in paths:
     target=candidate/path;target.parent.mkdir(parents=True,exist_ok=True);target.write_text('{}' if path.endswith('.json') else 'export type Amount = string;')
    patch=build_patch(base,candidate,[lane],root/'patch')
    self.assertEqual({e['path'] for e in patch['entries']},set(paths))
    stage_patches(base,[root/'patch'],root/'stage',verify=lambda p: all((p/x).read_bytes()==(candidate/x).read_bytes() for x in paths),ownership={patch['patch_digest']:[lane]})
    # Exception is not an authorization: foreign lane still cannot submit it.
    with self.assertRaises(PatchError):build_patch(base,candidate,['unrelated/**'],root/'foreign')
 def test_root_lock_schema_generated_and_path_escapes_stay_denied(self):
  paths=['package.json','client/package.json','client/apps/web/package.json','client/packages/sdk/nested/package.json','client/apps/mobile/nested/package.json','client/packages/sdk/pnpm-lock.yaml','client/apps/mobile/package-lock.json','pnpm-workspace.yaml','client/packages/sdk/src/other.gen.ts','client/apps/mobile/types.gen.ts','client/packages/sdk/types.gen.ts','client/packages/sdk/src/schema.gen.go','client/packages/production-contracts/src/types.gen.ts','spec/production-system/integration/contracts/v1/openapi.json','shared/interfaces/schema.json','client/packages/sdk/migrations/001.sql']
  for path in paths:
   with self.subTest(path=path),tempfile.TemporaryDirectory() as tmp:
    self.assertTrue(_reserved(path))
    root=Path(tmp);base=root/'base';base.mkdir();candidate=root/'candidate';candidate.mkdir();target=candidate/path;target.parent.mkdir(parents=True,exist_ok=True);target.write_text('forbidden')
    with self.assertRaises(PatchError):build_patch(base,candidate,[str(Path(path).parent)+'/**'],root/'patch')
  with tempfile.TemporaryDirectory() as tmp:
   root=Path(tmp);base=root/'base';base.mkdir();candidate=root/'candidate';candidate.mkdir()
   for pattern in ['client/packages/sdk/../sdk/package.json','/client/packages/sdk/package.json','client/packages/sdk\\package.json']:
    with self.subTest(pattern=pattern),self.assertRaises(PatchError):build_patch(base,candidate,[pattern],root/'patch')

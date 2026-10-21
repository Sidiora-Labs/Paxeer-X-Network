"""Executable filesystem qualification scenarios for isolated patch staging."""
import hashlib
import json
import os
from pathlib import Path
import shutil
from .patches import PatchError, build_patch, stage_patches


def qualify_paths(rootpath):
    root = Path(rootpath) / 'path-scenarios'
    root.mkdir(parents=True, exist_ok=False)
    baseline = root / 'baseline'
    (baseline / 'service').mkdir(parents=True)
    (baseline / 'service/modify').write_text('original')
    (baseline / 'service/delete').write_text('delete me')
    (baseline / 'service/rename-old').write_text('rename me')
    original = {p.relative_to(baseline).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in baseline.rglob('*') if p.is_file()}
    outcomes = []

    def record(name, success, **evidence):
        outcomes.append({'case': name, 'passed': bool(success), **evidence})
        if not success:
            raise AssertionError('path qualification failed: ' + name)

    def candidate(name):
        target = root / name
        shutil.copytree(baseline, target)
        return target

    def rejected(name, operation):
        try:
            operation()
        except PatchError as error:
            record(name, True, error=str(error))
        else:
            record(name, False)

    changed = candidate('valid-candidate')
    (changed / 'service/modify').write_text('modified')
    (changed / 'service/add').write_text('added')
    (changed / 'service/delete').unlink()
    (changed / 'service/rename-old').rename(changed / 'service/rename-new')
    patch = build_patch(baseline, changed, ['service/**'], root / 'valid-patch')
    ownership = {patch['patch_digest']: ['service/**']}
    first = stage_patches(baseline, [patch], root / 'stage-first', lambda _: True, ownership)
    second = stage_patches(baseline, [patch], root / 'stage-second', lambda _: True, ownership)
    stage = root / 'stage-first'
    record('addition', (stage / 'service/add').read_text() == 'added')
    record('modification', (stage / 'service/modify').read_text() == 'modified')
    record('deletion', not (stage / 'service/delete').exists())
    record('rename', not (stage / 'service/rename-old').exists() and (stage / 'service/rename-new').read_text() == 'rename me')
    record('deterministic-stage', first['result_digest'] == second['result_digest'], result_digest=first['result_digest'])

    for index, reserved in enumerate(('service/package.json', 'service/go.mod', 'service/migrations/001.sql',
                                      'service/internal/db/sqlcdb/query.go', 'service/cmd/api/main.go',
                                      'service/internal/app/app.go', 'spec/production-system/spec.kvx',
                                      'shared/interfaces/v1/types.gen.go', 'go.work', 'client/pnpm-workspace.yaml')):
        tree = candidate('reserved-candidate-' + str(index))
        target = tree / reserved
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text('forbidden')
        rejected('reserved-' + str(index), lambda tree=tree, index=index: build_patch(baseline, tree, ['service/**'], root / ('reserved-patch-' + str(index))))

    for index, pattern in enumerate(('../service/**', 'service/../../**', str(baseline / 'service/**'), 'C:/service/**')):
        rejected('unsafe-path-' + str(index), lambda pattern=pattern, index=index: build_patch(baseline, changed, [pattern], root / ('unsafe-patch-' + str(index))))

    symlink = candidate('symlink-candidate')
    (symlink / 'service/link').symlink_to(baseline / 'service/modify')
    rejected('symlink', lambda: build_patch(baseline, symlink, ['service/**'], root / 'symlink-patch'))
    hardlink = candidate('hardlink-candidate')
    os.link(hardlink / 'service/modify', hardlink / 'service/link')
    rejected('hardlink', lambda: build_patch(baseline, hardlink, ['service/**'], root / 'hardlink-patch'))

    left = candidate('overlap-left')
    right = candidate('overlap-right')
    (left / 'service/modify').write_text('left')
    (right / 'service/delete').write_text('right')
    left_patch = build_patch(baseline, left, ['service/**'], root / 'overlap-left-patch')
    right_patch = build_patch(baseline, right, ['service/**'], root / 'overlap-right-patch')
    overlap_ownership = {left_patch['patch_digest']: ['service/**'], right_patch['patch_digest']: ['service/**']}
    rejected('overlapping-ownership-distinct-files', lambda: stage_patches(baseline, [left_patch, right_patch], root / 'overlap-stage', lambda _: True, overlap_ownership))
    rejected('failed-verification', lambda: stage_patches(baseline, [patch], root / 'failed-stage', lambda _: False, ownership))
    current = {p.relative_to(baseline).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
               for p in baseline.rglob('*') if p.is_file()}
    record('failed-stage-preserves-baseline', original == current and not (root / 'failed-stage').exists(), baseline_files=current)
    rejected('unqualified-stage', lambda: stage_patches(baseline, [patch], root / 'unqualified-stage', None, ownership))
    artifact = {'checks': len(outcomes), 'passed': all(item['passed'] for item in outcomes),
                'case_outcomes': outcomes, 'artifact_root': str(root),
                'patch_digest': patch['patch_digest'], 'result_digest': first['result_digest']}
    (root / 'outcomes.json').write_text(json.dumps(artifact, indent=2) + '\n')
    return artifact

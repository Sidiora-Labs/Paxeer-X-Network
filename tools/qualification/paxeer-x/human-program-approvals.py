#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
SOURCES = (
    'human/apps/web/src/journeys/approvals/controller.ts',
    'human/apps/web/src/journeys/approvals/model.ts',
    'human/apps/web/src/journeys/approvals/screens.tsx',
    'human/apps/web/src/journeys/approvals/journey-screen.tsx',
    'human/apps/web/src/journeys/approvals/programs.tsx',
    'human/apps/web/src/journeys/home/model.ts',
    'human/apps/web/src/journeys/home/home.tsx',
    'human/apps/web/src/journeys/notifications/controller.ts',
    'human/apps/web/src/journeys/notifications/store.tsx',
    'human/apps/web/src/app/app/approvals/[approvalId]/page.tsx',
    'human/apps/web/e2e/program-approvals.test.ts',
    'tools/qualification/paxeer-x/human-program-approvals.py',
    'human/apps/web/src/api/generated/index.ts',
    'human/apps/web/src/api/generated/conformance.ts',
    'human/apps/web/copy/catalog.ts',
    'human/apps/web/copy/messages.generated.ts',
)


def require(value, message):
    if not value:
        raise RuntimeError(message)


def main():
    path = os.environ.get('PAXEER_X_HUMAN_PROGRAM_APPROVAL_FIXTURE')
    require(path, 'genuine protected Human/Programs/browser fixture is required')
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 65_536 and info.st_mode & 0o077 == 0,
            'genuine fixture must be protected and bounded')
    fixture = json.loads(path.read_text())
    require(fixture.get('schema') == 'paxeer-x.human-program-approvals.v1', 'invalid real owner fixture schema')
    require(fixture.get('isolated_real_owner') is True, 'isolated genuine owner process is required')
    hashes = fixture.get('source_hashes', {})
    require(all(hashes.get(source) == hashlib.sha256((ROOT / source).read_bytes()).hexdigest()
                for source in SOURCES), 'genuine owner/browser fixture does not bind the final source')
    require((ROOT / 'human/apps/web/.next/BUILD_ID').is_file(), 'actual production web build is required')
    require(Path(fixture.get('browser_executable', '')).is_file(), 'genuine browser executable is required')
    result = subprocess.run(['/root/lx-toolchains/node24/bin/node', '--test',
                             'e2e/program-approvals.test.ts'], cwd=ROOT / 'human/apps/web',
                            env=os.environ, timeout=480, capture_output=True, check=False)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    require(result.returncode == 0 and b'pass 1' in result.stdout and b'fail 0' in result.stdout,
            'genuine generated client, permission UI and durable push gate failed or did not execute')


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as error:
        print('Programs approval UI refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        print('Programs approval UI refused: genuine owner or browser process unavailable', file=sys.stderr)
        sys.exit(1)

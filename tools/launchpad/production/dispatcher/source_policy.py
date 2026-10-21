"""Shared source boundary: generated state and secrets are never source inputs.

Locks/manifests remain included; dependencies/tool binaries are separately bound
by the frozen interface bundle and actual executed-check receipt hashes.
"""
from pathlib import PurePosixPath
SOURCE_POLICY_VERSION='qualification-source-v1'
IGNORED_DIRS=frozenset({'.git','.codegraph','node_modules','.next','.gradle','.turbo','.cache','__pycache__','.pytest_cache','coverage','evidence','playwright-report','test-results','dist','.venv','venv','secrets','.secrets','.aws','.ssh','.gnupg'})
IGNORED_FILES=frozenset({'coverage.out','.coverage','.DS_Store','tsconfig.tsbuildinfo'})
def ignored(relative):
    parts=PurePosixPath(str(relative)).parts
    if parts[:2] == ('shared', 'secrets') and len(parts) > 2 and (len(parts) != 3 or not parts[-1].endswith('.go')):
        return True
    return any((p in IGNORED_DIRS and not (i == 1 and ((p == 'coverage' and parts[0] == 'protocol') or (p == 'secrets' and parts[0] == 'shared')))) or p=='.env' or p.startswith('.env.') for i,p in enumerate(parts)) or bool(parts and (parts[-1] in IGNORED_FILES or parts[-1].endswith(('.pyc','.prof','.test','.tsbuildinfo','.pem','.key','.p12','.pfx'))))

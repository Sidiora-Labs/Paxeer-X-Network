import { readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { join, relative } from 'node:path';
import { describe, expect, it } from 'vitest';
import ts from 'typescript';

const SOURCE_FILES = [
  'src/app',
  'src/components',
  'src/hooks',
  'src/lib',
  'src/platform',
  'src/providers',
  'src/widgets',
];

const APPROVED_DIRECT_STORAGE = new Set([
  'src/lib/wallet/PaxeerWallet.ts',
  'src/lib/wallet/v2/adapters/indexeddb-storage-adapter.ts',
  'src/lib/wallet/v2/core/legacy-migration-manager.ts',
  'src/platform/storage/registry.ts',
]);

function sourceFiles(): string[] {
  return execFileSync('find', [...SOURCE_FILES, '-type', 'f', '(', '-name', '*.ts', '-o', '-name', '*.tsx', ')'], {
    encoding: 'utf8',
  })
    .trim()
    .split('\n')
    .filter(Boolean);
}

function accessesBrowserStorage(file: string): boolean {
  const source = ts.createSourceFile(
    file,
    readFileSync(file, 'utf8'),
    ts.ScriptTarget.Latest,
    true,
  );
  let found = false;
  const visit = (node: ts.Node) => {
    if (
      ts.isIdentifier(node) &&
      (node.text === 'localStorage' || node.text === 'sessionStorage')
    ) {
      found = true;
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return found;
}

describe('storage forensics', () => {
  it('keeps direct browser storage access inside registered or wallet-core boundaries', () => {
    const violations = sourceFiles()
      .filter((file) => !file.includes('.test.'))
      .filter(accessesBrowserStorage)
      .map((file) => relative(process.cwd(), join(process.cwd(), file)))
      .filter((file) => !APPROVED_DIRECT_STORAGE.has(file));
    expect(violations).toEqual([]);
  });
});

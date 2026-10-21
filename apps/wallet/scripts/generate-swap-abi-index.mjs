import { readdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const directory = path.join(root, 'src/lib/swap/sdk/abis');
const outputPath = path.join(directory, 'index.ts');
const sourceFiles = (await readdir(directory))
  .filter((name) => name.endsWith('.ts') && name !== 'index.ts')
  .sort((left, right) => left.localeCompare(right));

const exports = [];
for (const file of sourceFiles) {
  const source = await readFile(path.join(directory, file), 'utf8');
  const names = [...source.matchAll(/export const ([A-Z0-9_]+_ABI)\b/g)].map(
    (match) => match[1],
  );
  if (names.length === 0) {
    throw new Error(`No ABI export found in ${file}`);
  }
  const moduleName = `./${file.slice(0, -3)}`;
  exports.push(`export { ${names.join(', ')} } from '${moduleName}';`);
}

const generated = `${exports.join('\n')}\n`;
if (process.argv.includes('--check')) {
  const current = await readFile(outputPath, 'utf8');
  if (current !== generated) {
    throw new Error('Swap ABI index is stale; run npm run generate:swap-abis');
  }
} else {
  await writeFile(outputPath, generated, 'utf8');
}

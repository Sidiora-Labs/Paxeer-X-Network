import { readdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, extname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const RELATIVE_SPECIFIER_RE = /(from\s+["']|import\(\s*["'])(\.{1,2}\/[^"']+)(["'])/g;
// tsc's declaration emitter collapses a `/** @ts-ignore */` JSDoc onto the same line as
// the declaration it documents. `@ts-ignore` only suppresses the next line, so the
// collapsed form leaves the speculative parent-node_modules import ladders in
// internal/types.d.ts unsuppressed and consumers with `skipLibCheck: false` fail with
// TS2307. The trailing space keeps this from matching comments already on their own line.
const INLINE_TS_IGNORE_RE = /\/\*\* @ts-ignore\b[^*]*\*\/ /g;
// `tsc` opens every CommonJS module by tagging the exports object as an ES module. That line is
// the seam the callable-entry shim below splices itself in front of: everything `tsc` emits after
// it attaches the named exports to whatever `exports` refers to at that point.
const ESM_MARKER_RE = /^([ \t]*Object\.defineProperty\(exports, ["']__esModule["'].*)$/m;
// Spliced ahead of the marker above so `tsc`'s named-export definitions attach to this function.
// `exports.default` is read at call time, not captured, because `tsc` defines it as a getter on a
// later line — reading it eagerly here would capture `undefined`.
//
// `tsc`'s `__exportStar` helper skips any key the exports object already owns, so every own property
// of the callable silently shadows an `export *` binding of that name in the CommonJS build alone —
// callers get the function's own value back, with no error anywhere. A plain function owns `name`,
// `length` and `prototype`; the first two are configurable and are deleted, but `prototype` is not,
// which is why the callable is a *bound* function: binding produces an exotic function object that
// owns no `prototype` at all, leaving nothing to shadow. `new` and plain calls both still forward.
//
// Kept on one line, and prefixed onto the marker rather than inserted above it, so the splice adds
// no lines: `tsc` has already written `index.js.map` by this point, and shifting lines would leave
// every mapping past the splice pointing at the wrong statement.
const CALLABLE_ENTRY_SHIM =
  'exports = module.exports = (function (...args) { return new exports.default(...args) }).bind(null); ' +
  'delete exports.name; delete exports.length; ';

const commonJsDir = resolve(root, 'dist/cjs');

await Promise.all([
  finalizeCompiledOutput(resolve(root, 'dist/esm'), { addJsExtensions: true }),
  // Sequenced, not concurrent: `finalizeCompiledOutput` also writes into this directory (`.d.ts`
  // rewrites today), so the steps after it never race it for a file it may also touch.
  finalizeCompiledOutput(commonJsDir, { addJsExtensions: false })
    .then(() => markCommonJsOutput(commonJsDir))
    .then(() => makeCommonJsEntryCallable(commonJsDir)),
]);

/**
 * Applies post-`tsc` fixups to one compiled output directory: extensionless relative
 * specifiers get a `.js` suffix in the ESM output, and declaration files get collapsed
 * inline `@ts-ignore` JSDoc rewritten to a line comment so the suppression still works.
 */
async function finalizeCompiledOutput(dir, { addJsExtensions }) {
  let entries;
  try {
    entries = await readdir(dir, { withFileTypes: true });
  } catch (error) {
    if (error && error.code === 'ENOENT') return;
    throw error;
  }

  await Promise.all(
    entries.map(async (entry) => {
      const path = resolve(dir, entry.name);
      if (entry.isDirectory()) {
        await finalizeCompiledOutput(path, { addJsExtensions });
        return;
      }
      if (!path.endsWith('.js') && !path.endsWith('.d.ts')) return;
      const source = await readFile(path, 'utf8');
      let transformed = source;
      if (addJsExtensions) transformed = transformed.replace(RELATIVE_SPECIFIER_RE, addJsExtension);
      if (path.endsWith('.d.ts')) transformed = transformed.replace(INLINE_TS_IGNORE_RE, '// @ts-ignore\n');
      if (transformed !== source) await writeFile(path, transformed, 'utf8');
    }),
  );
}

/**
 * Makes the CommonJS entry callable as a constructor, so `new (require('<package>'))()` builds a
 * client the same way `new Client()` does after an ESM import.
 *
 * CommonJS consumers written before this package shipped named exports call the module itself.
 * `tsc` emits a plain exports object, which is not callable, so that form dies with "is not a
 * constructor" — and it is the one shape a type declaration cannot describe, so nothing warns the
 * caller at compile time. Reassigning `module.exports` to a function that forwards to the default
 * export restores it while keeping every named export: the assignment is spliced in *before*
 * `tsc`'s own `Object.defineProperty(exports, ...)` calls, so those land on the function object and
 * `require(...).default`, `require(...).APIError` and friends keep resolving.
 *
 * A missing entry or a missing marker is left alone rather than thrown on. This runs inside the
 * consumer's own `npm run build`, so a `tsc` output-shape change must not turn a lost interop
 * convenience into a build that fails for everyone; the generator's CommonJS e2e coverage is what
 * catches the marker going missing.
 */
async function makeCommonJsEntryCallable(dir) {
  const entry = resolve(dir, 'index.js');
  let source;
  try {
    source = await readFile(entry, 'utf8');
  } catch (error) {
    if (error && error.code === 'ENOENT') return;
    throw error;
  }

  if (source.includes(CALLABLE_ENTRY_SHIM)) return;
  if (!ESM_MARKER_RE.test(source)) return;

  await writeFile(entry, source.replace(ESM_MARKER_RE, `${CALLABLE_ENTRY_SHIM}$1`), 'utf8');
}

async function markCommonJsOutput(dir) {
  try {
    await readdir(dir);
    await writeFile(resolve(dir, 'package.json'), '{\n  "type": "commonjs"\n}\n', 'utf8');
  } catch (error) {
    if (error && error.code === 'ENOENT') return;
    throw error;
  }
}

function addJsExtension(match, prefix, specifier, suffix) {
  if (extname(specifier)) return match;
  return `${prefix}${specifier}.js${suffix}`;
}

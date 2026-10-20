// Keep browser HTTP calls behind the bounded transport. Server API handlers
// have their own upstream policy and are outside this browser-only check.
const fs = require('node:fs');
const path = require('node:path');
const ts = require('typescript');
const failures = [];
function visit(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name);
    if (entry.isDirectory()) { visit(file); continue; }
    if (!/\.[jt]sx?$/.test(file) || file.includes('/pages/api/') || file.endsWith('/core/net/fetch.ts')) continue;
    const source = fs.readFileSync(file, 'utf8');
    const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true);
    const allowed = new Set();
    for (const node of ast.statements) {
      if (!ts.isImportDeclaration(node) || !String(node.moduleSpecifier.text).endsWith('/core/net/fetch')) continue;
      const bindings = node.importClause?.namedBindings;
      if (bindings && ts.isNamedImports(bindings)) for (const item of bindings.elements) allowed.add(item.name.text);
    }
    function walk(node) {
      if (ts.isCallExpression(node)) {
        const fn = node.expression;
        if ((ts.isIdentifier(fn) && fn.text === 'fetch' && !allowed.has('fetch')) ||
            (ts.isPropertyAccessExpression(fn) && ['window', 'globalThis', 'self'].includes(fn.expression.getText(ast)) && fn.name.text === 'fetch')) {
          failures.push(`${file}:${ast.getLineAndCharacterOfPosition(node.getStart()).line + 1}`);
        }
      }
      ts.forEachChild(node, walk);
    }
    walk(ast);
  }
}
visit('src');
if (failures.length) { console.error('Unbounded browser fetch:', ...failures); process.exitCode = 1; }
else console.log('Browser HTTP transport check passed');

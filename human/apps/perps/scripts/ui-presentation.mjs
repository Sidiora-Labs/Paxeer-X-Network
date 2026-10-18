import ts from "typescript";
import { createHash } from "node:crypto";
export const hash = (value) => createHash("sha256").update(value).digest("hex");
export function runtime(file, contents) {
  if (file.endsWith(".css")) return contents;
  if (file === "packages/layout-grid/src/GridRenderer.tsx") {
    const source = ts.createSourceFile(file, contents, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const roots = [];
    function collect(node) {
      if (ts.isJsxElement(node) || ts.isJsxSelfClosingElement(node) || ts.isJsxFragment(node)) { roots.push(node); return; }
      ts.forEachChild(node, collect);
    }
    collect(source);
    const bindings = new Set(["onLayoutChange", "onDragStart", "onDragStop", "onResizeStart", "onResizeStop"]);
    const transform = (context) => {
      const visit = (node) => {
        if ((ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) && node.tagName.getText(source) === "GridLayout") {
          const attrs = ts.factory.updateJsxAttributes(node.attributes, node.attributes.properties.filter((attr) => !ts.isJsxAttribute(attr) || !bindings.has(attr.name.getText(source))));
          node = ts.isJsxOpeningElement(node)
            ? ts.factory.updateJsxOpeningElement(node, node.tagName, node.typeArguments, attrs)
            : ts.factory.updateJsxSelfClosingElement(node, node.tagName, node.typeArguments, attrs);
        }
        return ts.visitEachChild(node, visit, context);
      };
      return (node) => ts.visitNode(node, visit);
    };
    const printer = ts.createPrinter({ removeComments: true });
    contents = roots.map((node) => {
      const result = ts.transform(node, [transform]);
      const text = printer.printNode(ts.EmitHint.Unspecified, result.transformed[0], source);
      result.dispose(); return text;
    }).join(";\n");
  }
  return ts.transpileModule(contents, { fileName: file, compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext, jsx: ts.JsxEmit.Preserve, removeComments: true } }).outputText;
}

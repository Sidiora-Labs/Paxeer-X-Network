import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { describe, expect, it } from 'vitest';

const EMOJI =
  /[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]/u;
const DISALLOWED_VISUAL =
  /(purple|violet|fuchsia|drop-shadow|radial-gradient|glow)/i;

function productionComponents(): string[] {
  return execFileSync(
    'find',
    ['src', '-type', 'f', '(', '-name', '*.tsx', '-o', '-name', '*.ts', ')'],
    { encoding: 'utf8' },
  )
    .trim()
    .split('\n')
    .filter((file) => file && !file.includes('.test.'));
}

function renderedTextAndClasses(file: string): string[] {
  const source = ts.createSourceFile(
    file,
    readFileSync(file, 'utf8'),
    ts.ScriptTarget.Latest,
    true,
    file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const values: string[] = [];
  const visit = (node: ts.Node) => {
    if (
      ts.isStringLiteral(node) ||
      ts.isNoSubstitutionTemplateLiteral(node) ||
      ts.isTemplateHead(node) ||
      ts.isTemplateMiddle(node) ||
      ts.isTemplateTail(node) ||
      ts.isJsxText(node)
    ) {
      values.push(node.text);
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return values;
}

describe('professional visual system', () => {
  it('keeps production UI free of border utilities, purple styling, glow, and emoji', () => {
    const violations: string[] = [];
    for (const file of productionComponents()) {
      for (const value of renderedTextAndClasses(file)) {
        const hasBorderClass = value.split(/\s+/).some((token) =>
          /(?:^|:)border(?:-|$)|(?:^|:)divide-[xy](?:-|$)/.test(token),
        );
        if (
          hasBorderClass ||
          DISALLOWED_VISUAL.test(value) ||
          EMOJI.test(value)
        ) {
          violations.push(file);
          break;
        }
      }
    }
    expect([...new Set(violations)]).toEqual([]);
  });

  it('defines every required token family and reusable primitive', () => {
    const css = readFileSync('src/app/globals.css', 'utf8');
    for (const token of [
      '--color-surface-base',
      '--color-text-primary',
      '--color-action-primary',
      '--color-status-success',
      '--font-size-md',
      '--space-4',
      '--density-control',
      '--touch-target',
      '--motion-normal',
      '--elevation-sheet',
      '--safe-area-top',
    ]) {
      expect(css).toContain(token);
    }
    expect(css).not.toMatch(/\bborder\s*:\s*(?!none\b)/);
    expect(css).not.toMatch(/\bborder-(?:top|right|bottom|left)\s*:\s*(?!none\b)/);

    const primitives = readFileSync('src/components/ui/primitives.tsx', 'utf8');
    for (const name of [
      'Button',
      'TextField',
      'AmountText',
      'SegmentedControl',
      'Switch',
      'Dialog',
      'Sheet',
      'Menu',
      'ToastRegion',
      'Skeleton',
      'ErrorState',
      'ApprovalSummary',
    ]) {
      expect(primitives).toMatch(new RegExp(`export (?:const|function) ${name}\\b`));
    }
  });
});

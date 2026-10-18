// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { columnLimits, defaultColumns, layoutColumns, parseColumns, resetTerminalColumns, terminalColumnsStorageKey, useTerminalColumns, type TerminalColumns } from './column-store';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

let latest: TerminalColumns | null = null;
function Probe() {
  latest = useTerminalColumns();
  return <output>{`${latest.leftWidth}|${latest.rightWidth}|${latest.leftCollapsed}|${latest.rightCollapsed}`}</output>;
}

let mounted: { root: Root; container: HTMLDivElement } | null = null;
function mount() {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(<Probe />));
  mounted = { root, container };
  return container;
}

beforeEach(() => {
  window.localStorage.clear();
  act(() => resetTerminalColumns());
});
afterEach(() => {
  if (mounted) { const { root, container } = mounted; act(() => root.unmount()); container.remove(); mounted = null; }
  latest = null;
});

describe('parseColumns', () => {
  it('falls back to defaults for missing or malformed storage', () => {
    expect(parseColumns(null)).toEqual(defaultColumns);
    expect(parseColumns('not json')).toEqual(defaultColumns);
    expect(parseColumns('[1,2]')).toEqual(defaultColumns);
  });
  it('clamps widths to the column minimums and the side maximum', () => {
    expect(parseColumns(JSON.stringify({ leftWidth: 10, rightWidth: 9000, leftCollapsed: true, rightCollapsed: 'yes' }))).toEqual({
      leftWidth: columnLimits.leftMin, rightWidth: columnLimits.sideMax, leftCollapsed: true, rightCollapsed: false,
    });
  });
});

describe('useTerminalColumns', () => {
  it('persists widths and collapsed flags under sidiora-terminal-columns', () => {
    const container = mount();
    expect(container.textContent).toBe('296|312|false|false');
    act(() => latest?.setLeftWidth(400));
    act(() => latest?.toggleRight());
    expect(container.textContent).toBe('400|312|false|true');
    expect(JSON.parse(window.localStorage.getItem(terminalColumnsStorageKey) ?? '{}')).toEqual({ leftWidth: 400, rightWidth: 312, leftCollapsed: false, rightCollapsed: true });
    act(() => latest?.setRightWidth(100));
    expect(container.textContent).toBe('400|280|false|true');
    act(() => resetTerminalColumns());
    expect(container.textContent).toBe('296|312|false|false');
  });
  it('picks up a change written by another tab', () => {
    const container = mount();
    window.localStorage.setItem(terminalColumnsStorageKey, JSON.stringify({ leftWidth: 320, rightWidth: 360, leftCollapsed: true, rightCollapsed: false }));
    act(() => { window.dispatchEvent(new StorageEvent('storage', { key: terminalColumnsStorageKey })); });
    expect(container.textContent).toBe('320|360|true|false');
  });
});

describe('layoutColumns', () => {
  const left = { present: true, width: 296, collapsed: false, min: 240 };
  const right = { present: true, width: 312, collapsed: false, min: 280 };
  it('keeps stored widths when the centre has room', () => {
    const layout = layoutColumns(1400, left, right);
    expect(layout.left).toMatchObject({ mode: 'open', width: 296, autoCollapsed: false });
    expect(layout.right).toMatchObject({ mode: 'open', width: 312, autoCollapsed: false });
    expect(layout.left.max).toBe(1400 - 16 - 560 - 312);
    expect(layoutColumns(2400, left, right).left.max).toBe(columnLimits.sideMax);
  });
  it('shrinks the right then the left column toward their minimums before collapsing', () => {
    const layout = layoutColumns(1150, left, right);
    expect(layout.right.width).toBe(280);
    expect(layout.left.width).toBe(1150 - 16 - 560 - 280);
  });
  it('collapses the right column for this render when even the minimums do not fit', () => {
    const layout = layoutColumns(968, left, right);
    expect(layout.right).toMatchObject({ mode: 'collapsed', width: 24, autoCollapsed: true });
    expect(layout.left.mode).toBe('open');
    expect(layout.left.width + 24 + 560 + 16).toBeLessThanOrEqual(968);
  });
  it('treats missing sides as absent', () => {
    const layout = layoutColumns(800, { ...left, present: false }, { ...right, present: false });
    expect(layout.left.mode).toBe('absent');
    expect(layout.right.mode).toBe('absent');
  });
});

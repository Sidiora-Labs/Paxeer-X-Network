// @vitest-environment jsdom
import React, { act, useRef, useState } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import PlatformRulesModal, { RULES_AGREED_KEY } from '@/ui/shared/platform-rules-modal';
import { useDialogFocus } from './use-dialog-focus';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

const roots: Root[] = [];
beforeEach(() => { window.localStorage.clear(); });
afterEach(() => {
  for (const root of roots.splice(0)) act(() => root.unmount());
  document.body.innerHTML = '';
  window.localStorage.clear();
});

function mount(node: React.ReactNode) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  roots.push(root);
  act(() => root.render(node));
  return root;
}

/** Same shape as the private-session wrapper in ui/providers/query-provider.tsx: React owns `inert` / `aria-hidden`. */
let setBlocked: (value: boolean) => void = () => {};
function SessionWrapper() {
  const [blocked, update] = useState(true);
  setBlocked = update;
  return (
    <div data-testid="app-root" inert={blocked} aria-hidden={blocked || undefined}>
      <a href="/explore">Explore</a>
    </div>
  );
}

const appRoot = () => document.querySelector<HTMLElement>('[data-testid="app-root"]')!;
const agree = () => Array.from(document.querySelectorAll('button')).find(button => button.textContent?.includes('I understand and agree'))!;

describe('useDialogFocus background ownership', () => {
  it('first visit: rules modal over a React-managed inert wrapper leaves the app interactive after accepting', () => {
    mount(<><SessionWrapper /><PlatformRulesModal /></>);
    // 1. wrapper inert (session still initialising) and 2. the modal is open over it.
    expect(document.querySelector('[role="dialog"]')).not.toBeNull();
    expect(appRoot().hasAttribute('inert')).toBe(true);
    // 3. React clears its own inert while the modal is still open.
    act(() => setBlocked(false));
    // 4. Accept closes the modal.
    act(() => agree().click());
    expect(window.localStorage.getItem(RULES_AGREED_KEY)).not.toBeNull();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    // 5. The app root must not be left inert or hidden.
    expect(appRoot().hasAttribute('inert')).toBe(false);
    expect(appRoot().hasAttribute('aria-hidden')).toBe(false);
    expect(document.querySelectorAll('body [inert]')).toHaveLength(0);
  });

  it('adds inert to an interactive background and removes it on close', () => {
    let open: (value: boolean) => void = () => {};
    function Dialog() {
      const [isOpen, update] = useState(false);
      open = update;
      const ref = useRef<HTMLDivElement>(null);
      useDialogFocus(ref, isOpen);
      return isOpen ? <div ref={ref} role="dialog"><button type="button">Close</button></div> : null;
    }
    mount(<><div data-testid="bg">page</div><Dialog /></>);
    const bg = document.querySelector<HTMLElement>('[data-testid="bg"]')!;
    act(() => open(true));
    expect(bg.getAttribute('inert')).toBe('');
    expect(bg.getAttribute('aria-hidden')).toBe('true');
    act(() => open(false));
    expect(bg.hasAttribute('inert')).toBe(false);
    expect(bg.hasAttribute('aria-hidden')).toBe(false);
  });

  it('never removes attributes it did not add', () => {
    let open: (value: boolean) => void = () => {};
    function Dialog() {
      const [isOpen, update] = useState(false);
      open = update;
      const ref = useRef<HTMLDivElement>(null);
      useDialogFocus(ref, isOpen);
      return isOpen ? <div ref={ref} role="dialog"><button type="button">Close</button></div> : null;
    }
    mount(<><div data-testid="bg" aria-hidden="true">decor</div><Dialog /></>);
    const bg = document.querySelector<HTMLElement>('[data-testid="bg"]')!;
    act(() => open(true));
    act(() => open(false));
    expect(bg.getAttribute('aria-hidden')).toBe('true');
    expect(bg.hasAttribute('inert')).toBe(false);
  });

  it('keeps the background inert until the last of two overlapping dialogs closes, in either order', () => {
    const setters: Record<string, (value: boolean) => void> = {};
    function Dialog({ id }: { id: string }) {
      const [isOpen, update] = useState(false);
      setters[id] = update;
      const ref = useRef<HTMLDivElement>(null);
      useDialogFocus(ref, isOpen);
      return isOpen ? <div ref={ref} role="dialog" aria-label={id}><button type="button">{id}</button></div> : null;
    }
    mount(<><div data-testid="bg">page</div></>);
    mount(<Dialog id="a" />);
    mount(<Dialog id="b" />);
    const bg = document.querySelector<HTMLElement>('[data-testid="bg"]')!;
    act(() => setters.a(true));
    act(() => setters.b(true));
    act(() => setters.a(false));
    expect(bg.closest('[inert]')).not.toBeNull();
    act(() => setters.b(false));
    expect(bg.closest('[inert]')).toBeNull();
    expect(document.querySelectorAll('body [inert], body [aria-hidden]')).toHaveLength(0);
  });
});

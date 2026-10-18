'use client';

import { useEffect, useState } from 'react';

/**
 * Elects one owner among several mounted instances of the same singleton UI
 * (the search palette and the sign-in dialog are rendered wherever their
 * triggers live, but only one dialog and one global key listener may exist).
 */
export function createSingleMount() {
  const mounted: symbol[] = [];
  const listeners = new Set<() => void>();
  const notify = () => { for (const listener of listeners) listener(); };
  return function useSingleMount(): boolean {
    const [token] = useState(() => Symbol('single-mount'));
    const [owner, setOwner] = useState(false);
    useEffect(() => {
      mounted.push(token);
      const update = () => setOwner(mounted[0] === token);
      listeners.add(update);
      notify();
      return () => {
        const index = mounted.indexOf(token);
        if (index >= 0) mounted.splice(index, 1);
        listeners.delete(update);
        notify();
      };
    }, [token]);
    return owner;
  };
}

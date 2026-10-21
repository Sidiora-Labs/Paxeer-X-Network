'use client';
import { createContext, useContext } from 'react';

/**
 * The header's action slot (left of the header icons) for the column a widget renders in. A widget
 * portals a header control (the callouts Live / Paused pill) into it instead of spending a tab row.
 * `undefined` outside a column (the widget renders the control inline); `null` inside a column
 * before the header has mounted (server render, first client render), when the widget renders
 * nothing there, so the body never shifts up once the control moves into the header.
 */
export const ColumnHeaderSlotContext = createContext<HTMLElement | null | undefined>(undefined);

export function useColumnHeaderSlot(): HTMLElement | null | undefined {
  return useContext(ColumnHeaderSlotContext);
}

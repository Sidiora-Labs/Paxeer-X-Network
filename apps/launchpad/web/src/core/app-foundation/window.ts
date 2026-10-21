export const MAX_MOUNTED_ROWS = 200;
/** Fixed row geometry makes DOM work independent of total market history. */
export function listWindow(count: number, rowHeight: number, viewport: number, offset: number, overscan = 5) {
  if (![count, rowHeight, viewport, offset, overscan].every(Number.isFinite) || !Number.isSafeInteger(count) || count < 0 || rowHeight <= 0 || viewport <= 0 || offset < 0 || !Number.isInteger(overscan) || overscan < 0) throw new Error('Invalid list geometry');
  if (Math.ceil(viewport / rowHeight) + 1 > MAX_MOUNTED_ROWS) throw new Error('Viewport exceeds row budget');
  const totalHeight = count * rowHeight;
  if (!Number.isSafeInteger(Math.ceil(totalHeight))) throw new Error('List exceeds safe geometry');
  const first = Math.min(Math.max(0, count - 1), Math.floor(Math.min(offset, Math.max(0, totalHeight - viewport)) / rowHeight));
  const spare = MAX_MOUNTED_ROWS - Math.ceil(viewport / rowHeight) - 1;
  const padding = Math.min(overscan, Math.floor(spare / 2));
  const start = Math.max(0, first - padding);
  const end = Math.min(count, first + Math.ceil(viewport / rowHeight) + 1 + padding);
  return { start, end, totalHeight };
}

import type { SplitLayoutNode } from "../types";

/**
 * Returns the split/sort node at the given path (used to read current state for change detection).
 *
 * @param node - Root node of the layout tree
 * @param path - Index path to the target node
 */
export function getNodeAtPath(
  node: SplitLayoutNode,
  path: number[],
): SplitLayoutNode | null {
  if (path.length === 0) return node;
  if (node.type === "panel") return null;
  const [childIndex, ...restPath] = path;
  if (childIndex >= node.children.length) return null;
  return getNodeAtPath(node.children[childIndex], restPath);
}

/**
 * Derives sizes array from split children (size per child).
 *
 * @param children - Split children nodes
 * @returns Array of size strings, defaulting to "auto" when missing
 */
export function getSizesFromChildren(children: SplitLayoutNode[]): string[] {
  return children.map((child) => child.size ?? "auto");
}

/**
 * Returns true if two size arrays are effectively equal (same rounded percentages).
 * Avoids propagating onLayoutChanged when react-resizable-panels reports the same layout on mount.
 *
 * @param current - Current sizes (from tree)
 * @param next - Next sizes (from resize callback)
 */
export function sizesAreEqual(
  current: string[] | undefined,
  next: string[],
): boolean {
  if (!current || current.length !== next.length) return false;
  const toNum = (value: string) =>
    Math.round(parseFloat(String(value).replace(/%/g, "")) || 0);
  return current.every((c, index) => toNum(c) === toNum(next[index]));
}

/**
 * Deep clone and update defaultCollapsed for a panel across the entire tree.
 * Updates ALL nodes with matching panelId (used when updating all breakpoints).
 */
export function updateDefaultCollapsedAtPath(
  node: SplitLayoutNode,
  panelId: string,
  defaultCollapsed: boolean,
): SplitLayoutNode {
  if (node.type === "panel" && node.id === panelId) {
    return { ...node, defaultCollapsed };
  }

  if (node.type === "split" || node.type === "sort") {
    return {
      ...node,
      children: node.children.map((child) =>
        updateDefaultCollapsedAtPath(child, panelId, defaultCollapsed),
      ),
    };
  }

  return node;
}

/**
 * Deep clone and update sizes at a specific path in the split tree.
 * When path targets a split, updates each child's size from the new sizes array.
 *
 * @param node - The root node
 * @param path - Array of indices representing the path to the target split
 * @param newSizes - Full sizes array from onSizeChange (updates children[].size)
 * @returns Updated node (immutable update)
 */
export function updateSizeAtPath(
  node: SplitLayoutNode,
  path: number[],
  newSizes: string[],
): SplitLayoutNode {
  if (node.type === "panel") {
    const size = newSizes[0];
    return size !== undefined ? { ...node, size } : node;
  }

  // Sort nodes don't have resizable children; recurse only if path continues.
  if (node.type === "sort") {
    if (path.length === 0) return node;
    const [childIndex, ...restPath] = path;
    const updatedChildren = [...node.children];
    if (childIndex < updatedChildren.length) {
      updatedChildren[childIndex] = updateSizeAtPath(
        updatedChildren[childIndex],
        restPath,
        newSizes,
      );
    }
    return { ...node, children: updatedChildren };
  }

  const updatedNode: SplitLayoutNode = {
    ...node,
    children: [...node.children],
  };

  if (path.length === 0) {
    updatedNode.children = node.children.map((child, index) => ({
      ...child,
      size: newSizes[index] ?? child.size ?? "auto",
    }));
    return updatedNode;
  }

  const [childIndex, ...restPath] = path;
  if (childIndex < updatedNode.children.length) {
    updatedNode.children[childIndex] = updateSizeAtPath(
      updatedNode.children[childIndex],
      restPath,
      newSizes,
    );
  }

  return updatedNode;
}

'use client';
import { useState, type DragEvent } from 'react';
import { cn } from '@/lib/utils';
import type { ColumnConfig, WidgetType } from './layout-store';
import { widgetRegistry } from './widget-registry';
import { ColumnChrome } from './column-chrome';
import { ColumnHeaderSlotContext } from './column-slot';

export const columnDragType = 'application/x-sidiora-column';

export interface ColumnProps {
  column: ColumnConfig;
  index: number;
  count: number;
  dragging: string | null;
  onDragStart(id: string): void;
  onDragEnd(): void;
  onDrop(id: string, toIndex: number): void;
  onMove(id: string, toIndex: number): void;
  onRemove(id: string): void;
  onChangeType(id: string, type: WidgetType): void;
  onPatch(id: string, patch: Partial<Omit<ColumnConfig, 'id'>>): void;
  /** Insert a new column at `atIndex` (the header adds right of this column). */
  onAdd?(type: WidgetType, atIndex: number): void;
  /** Fill the slot the parent sizes (board column slot, or the viewport in single-column mode) instead of the fixed token width. */
  fluid?: boolean;
  className?: string;
}

export function draggedColumnId(event: DragEvent<HTMLElement>): string | null {
  const id = event.dataTransfer.getData(columnDragType) || event.dataTransfer.getData('text/plain');
  return id ? id : null;
}

/**
 * One dashboard column (layout.md S03): a surface-1 card with the widget
 * chrome on top and the widget body scrolling inside. HTML5 drag and drop
 * reorders columns; the dragged card lifts to surface-3 with the pop shadow
 * and the drop target widens its leading gap.
 */
export function Column({ column, index, count, dragging, onDragStart, onDragEnd, onDrop, onMove, onRemove, onChangeType, onPatch, onAdd, fluid = false, className }: ColumnProps) {
  const definition = widgetRegistry[column.type];
  const Widget = definition.Component;
  const [over, setOver] = useState(false);
  const [headerSlot, setHeaderSlot] = useState<HTMLElement | null>(null);
  const isDragging = dragging === column.id;
  const dropTarget = over && dragging !== null && !isDragging;

  function start(event: DragEvent<HTMLElement>) {
    event.dataTransfer.setData(columnDragType, column.id);
    event.dataTransfer.setData('text/plain', column.id);
    event.dataTransfer.effectAllowed = 'move';
    onDragStart(column.id);
  }

  return (
    <section
      aria-label={definition.title}
      data-column-id={column.id}
      data-column-type={column.type}
      onDragOver={event => { if (dragging !== null && dragging !== column.id) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; setOver(true); } }}
      onDragEnter={() => { if (dragging !== null && dragging !== column.id) setOver(true); }}
      onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(false); }}
      onDrop={event => { event.preventDefault(); setOver(false); const id = draggedColumnId(event) ?? dragging; if (id) onDrop(id, index); }}
      className={cn(
        'relative flex h-full min-w-0 shrink-0 snap-start flex-col bg-surface-1 transition-[margin,background-color,box-shadow] duration-base',
        fluid ? 'w-full' : 'w-dashboard-column-width max-xl:w-[340px] max-lg:w-[320px]',
        isDragging && 'bg-surface-3 shadow-pop opacity-90',
        dropTarget && 'ml-4',
        className,
      )}
    >
      <ColumnChrome
        column={column}
        index={index}
        count={count}
        dragging={isDragging}
        onMove={toIndex => onMove(column.id, toIndex)}
        onRemove={() => onRemove(column.id)}
        onChangeType={type => onChangeType(column.id, type)}
        onPatch={patch => onPatch(column.id, patch)}
        onDragStart={start}
        onDragEnd={onDragEnd}
        onAdd={onAdd ? type => onAdd(type, index + 1) : undefined}
        actionsRef={setHeaderSlot}
      />
      <div role="region" aria-label={definition.title} className="flex min-h-0 flex-1 flex-col">
        <ColumnHeaderSlotContext.Provider value={headerSlot}>
          <Widget key={column.type} columnId={column.id} filter={column.filter} density={column.density} view={column.view} />
        </ColumnHeaderSlotContext.Provider>
      </div>
    </section>
  );
}

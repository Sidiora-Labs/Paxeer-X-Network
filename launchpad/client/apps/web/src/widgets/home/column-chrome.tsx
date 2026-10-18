'use client';
import { useEffect, useId, useRef, useState, type DragEvent, type KeyboardEvent } from 'react';
import { AlignJustify, ArrowLeft, ArrowRight, ChevronDown, Columns2, Ellipsis, Filter, GripVertical, LayoutGrid, List, Rows3, Trash2, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, DropdownMenu, IconButton, type MenuItem } from '@/ui/primitives';
import { maxColumns, type ColumnConfig, type WidgetType } from './layout-store';
import { widgetOrder, widgetRegistry } from './widget-registry';

export interface ColumnChromeProps {
  column: ColumnConfig;
  index: number;
  count: number;
  onMove(toIndex: number): void;
  onRemove(): void;
  onChangeType(type: WidgetType): void;
  onPatch(patch: Partial<Omit<ColumnConfig, 'id'>>): void;
  onDragStart(event: DragEvent<HTMLElement>): void;
  onDragEnd(): void;
  /** Add a column of `type` right of this one. Omitted, the header has no add-column control. */
  onAdd?(type: WidgetType): void;
  /** Receives the header's action slot element, where the widget can portal a header control. */
  actionsRef?(element: HTMLElement | null): void;
  dragging?: boolean;
}

/**
 * Widget card header and toolbar (components.md widget card with toolbar):
 * grip, type icon, title dropdown that changes the column type, filter
 * toggle revealing an inline input, density / view toggles where the widget
 * supports them, remove with an inline confirm, and a more menu.
 */
export function ColumnChrome({ column, index, count, onMove, onRemove, onChangeType, onPatch, onDragStart, onDragEnd, onAdd, actionsRef, dragging = false }: ColumnChromeProps) {
  const definition = widgetRegistry[column.type];
  const Icon = definition.icon;
  const [filterOpen, setFilterOpen] = useState(column.filter.length > 0);
  const [confirming, setConfirming] = useState(false);
  const filterId = useId();
  const filterRef = useRef<HTMLInputElement | null>(null);
  const confirmRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => { if (filterOpen) filterRef.current?.focus(); }, [filterOpen]);
  useEffect(() => { if (confirming) confirmRef.current?.querySelector('button')?.focus(); }, [confirming]);
  useEffect(() => { setConfirming(false); }, [column.type]);

  function onGripKey(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key === 'ArrowLeft' && index > 0) { event.preventDefault(); onMove(index - 1); }
    if (event.key === 'ArrowRight' && index < count - 1) { event.preventDefault(); onMove(index + 1); }
  }

  const typeItems: MenuItem[] = widgetOrder.map(type => {
    const TypeIcon = widgetRegistry[type].icon;
    return { id: type, label: widgetRegistry[type].title, icon: <TypeIcon aria-hidden="true" className="size-icon-sm" />, selected: type === column.type, onSelect: () => onChangeType(type) };
  });

  const full = count >= maxColumns;
  const addItems: MenuItem[] = full ? [] : widgetOrder.map(type => {
    const TypeIcon = widgetRegistry[type].icon;
    return { id: `add-${type}`, label: widgetRegistry[type].title, icon: <TypeIcon aria-hidden="true" className="size-icon-sm" />, onSelect: () => onAdd?.(type), group: 'Add column to the right' };
  });

  const moreItems: MenuItem[] = [
    { id: 'left', label: 'Move left', icon: <ArrowLeft aria-hidden="true" className="size-icon-sm" />, disabled: index === 0, onSelect: () => onMove(index - 1), group: 'Arrange' },
    { id: 'right', label: 'Move right', icon: <ArrowRight aria-hidden="true" className="size-icon-sm" />, disabled: index >= count - 1, onSelect: () => onMove(index + 1), group: 'Arrange' },
    { id: 'clear-filter', label: 'Clear filter', icon: <X aria-hidden="true" className="size-icon-sm" />, disabled: column.filter.length === 0, onSelect: () => { onPatch({ filter: '' }); setFilterOpen(false); }, group: 'Column' },
    { id: 'remove', label: 'Remove column', icon: <Trash2 aria-hidden="true" className="size-icon-sm" />, destructive: true, onSelect: () => setConfirming(true), group: 'Column' },
  ];

  return (
    <>
      {/* Size container: in a narrow column (under 340 px, e.g. 1024) the type icon and header control labels step aside so the title never truncates. */}
      <div className={cn('@container relative flex h-11 shrink-0 items-center gap-1.5 pl-1 pr-2', dragging && 'cursor-grabbing')}>
        <button
          type="button"
          aria-label="Reorder column"
          aria-describedby={`${filterId}-grip-hint`}
          draggable
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
          onKeyDown={onGripKey}
          className="inline-flex size-control-height-sm shrink-0 cursor-grab items-center justify-center rounded-sm text-text-faint hover:bg-surface-hover hover:text-text active:cursor-grabbing"
        >
          <GripVertical aria-hidden="true" className="size-icon-sm" />
        </button>
        <span id={`${filterId}-grip-hint`} className="sr-only">Drag, or press the left and right arrow keys, to move this column. Position {index + 1} of {count}.</span>
        <Icon aria-hidden="true" className="size-icon-sm shrink-0 text-text-muted @max-[340px]:hidden" />
        <h2 className="flex min-w-0 flex-1 items-center text-sm font-bold text-text">
          {/* Content-sized trigger: the caret-only default is a fixed square, which let the label spill left over the type icon. */}
          <DropdownMenu
            label={definition.title}
            menuLabel="Column type"
            items={typeItems}
            size="sm"
            className="min-w-0 max-w-full"
            trigger={<><span className="truncate">{definition.title}</span><ChevronDown aria-hidden="true" className="size-icon-sm shrink-0" /></>}
            triggerClassName="min-w-0 max-w-full justify-start gap-1 px-1.5 text-text"
          />
        </h2>
        {actionsRef ? <div ref={actionsRef} data-column-actions="" className="flex shrink-0 items-center empty:hidden" /> : null}
        <div className="flex shrink-0 items-center">
          {definition.supports.filter ? <IconButton size="sm" label={filterOpen ? 'Hide filter' : 'Filter'} pressed={filterOpen || column.filter.length > 0} icon={<Filter />} onClick={() => setFilterOpen(value => !value)} /> : null}
          {definition.supports.density ? <IconButton size="sm" label={column.density === 'compact' ? 'Comfortable rows' : 'Compact rows'} icon={column.density === 'compact' ? <Rows3 /> : <AlignJustify />} onClick={() => onPatch({ density: column.density === 'compact' ? 'comfortable' : 'compact' })} /> : null}
          {definition.supports.view ? <IconButton size="sm" label={column.view === 'cards' ? 'Show as list' : 'Show as cards'} icon={column.view === 'cards' ? <List /> : <LayoutGrid />} onClick={() => onPatch({ view: column.view === 'cards' ? 'list' : 'cards' })} /> : null}
          {onAdd ? (
            <DropdownMenu
              label={full ? `Column limit reached (${maxColumns})` : 'Add column'}
              menuLabel="Add column"
              items={addItems}
              size="sm"
              align="end"
              trigger={<Columns2 aria-hidden="true" className="size-icon-sm" />}
              triggerClassName="size-control-height-sm px-0"
            />
          ) : null}
          <IconButton size="sm" label="Remove column" icon={<Trash2 />} aria-expanded={confirming} onClick={() => setConfirming(value => !value)} />
          <DropdownMenu label="More" menuLabel={`${definition.title} column`} items={moreItems} size="sm" align="end" trigger={<Ellipsis aria-hidden="true" className="size-icon-sm" />} triggerClassName="size-control-height-sm px-0" />
        </div>
      </div>
      {confirming ? (
        <div ref={confirmRef} role="alertdialog" aria-label="Remove column?" className="flex h-10 shrink-0 items-center gap-2 bg-surface-2 px-3">
          <span className="flex-1 text-sm text-text">Remove column?</span>
          <Button variant="danger" size="sm" onClick={onRemove}>Remove</Button>
          <Button variant="ghost" size="sm" onClick={() => setConfirming(false)}>Keep</Button>
        </div>
      ) : null}
      {filterOpen && definition.supports.filter ? (
        <div className="relative flex h-10 shrink-0 items-center gap-1 px-2">
          <label htmlFor={filterId} className="sr-only">Filter {definition.title}</label>
          <input
            ref={filterRef}
            id={filterId}
            type="search"
            value={column.filter}
            onChange={event => onPatch({ filter: event.target.value.slice(0, 120) })}
            onKeyDown={event => { if (event.key === 'Escape') { onPatch({ filter: '' }); setFilterOpen(false); } }}
            placeholder={`Filter ${definition.title.toLowerCase()}`}
            autoComplete="off"
            className="h-control-height-sm min-w-0 flex-1 rounded-sm bg-surface-2 px-2 text-sm text-text placeholder:text-text-faint focus:bg-surface-3 focus:outline-none"
          />
          {column.filter ? <IconButton size="sm" label="Clear filter" icon={<X />} onClick={() => onPatch({ filter: '' })} /> : null}
        </div>
      ) : null}
    </>
  );
}

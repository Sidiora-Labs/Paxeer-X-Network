'use client';

import { useRef, useState, type DragEvent, type ReactNode } from 'react';
import { CircleAlert, ImagePlus, LoaderCircle, TriangleAlert } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button } from '@/ui/primitives';
import { MEDIA_TYPES, formatMiB, stageMedia, type StagedMedia, type StagedMediaDescriptor } from './media-validation';

export interface MediaDropzoneProps {
  id: string;
  staged: StagedMedia | null;
  onStaged: (staged: StagedMedia) => void;
  onClear: () => void;
  /** Descriptor restored from a saved draft when the file itself is no longer in memory. */
  remembered?: StagedMediaDescriptor | null;
  disabled?: boolean;
  headline?: string;
  footer?: ReactNode;
  className?: string;
}

export function MediaDropzone({ id, staged, onStaged, onClear, remembered = null, disabled = false, headline = 'Select image to upload', footer, className }: MediaDropzoneProps) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;

  async function accept(files: FileList | null) {
    const file = files?.[0];
    if (!file || disabled) return;
    setBusy(true);
    setError(null);
    try {
      onStaged(await stageMedia(file));
    } catch (cause) {
      console.error('[create] media', cause);
      setError("This file can't be used. Try another one.");
    } finally {
      setBusy(false);
      if (inputRef.current) inputRef.current.value = '';
    }
  }

  function onDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setDragOver(false);
    void accept(event.dataTransfer?.files ?? null);
  }

  const zoneTone = error ? 'bg-danger-soft' : dragOver ? 'bg-accent-soft' : 'bg-surface-2';

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <div
        onDragOver={event => { if (disabled) return; event.preventDefault(); setDragOver(true); }}
        onDragLeave={() => setDragOver(false)}
        onDrop={onDrop}
        className={cn('rounded-lg p-4 transition-colors duration-fast', zoneTone)}
      >
        <input
          ref={inputRef}
          id={id}
          type="file"
          accept={MEDIA_TYPES.join(',')}
          className="sr-only"
          disabled={disabled || busy}
          aria-describedby={error ? errorId : hintId}
          aria-invalid={error ? true : undefined}
          onChange={event => void accept(event.target.files)}
        />
        {staged ? (
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
            <img src={staged.previewUrl} alt={`Selected image ${staged.descriptor.name}`} width={120} height={120} className="size-30 shrink-0 rounded-md bg-surface-3 object-cover" />
            <div className="flex min-w-0 flex-1 flex-col gap-2">
              <p className="truncate text-sm font-bold text-text" title={staged.descriptor.name}>{staged.descriptor.name}</p>
              <p className="text-xs text-text-faint">
                {staged.descriptor.type.replace('image/', '').toUpperCase()}, {formatMiB(staged.descriptor.size)}
                {staged.descriptor.width && staged.descriptor.height ? `, ${staged.descriptor.width} x ${staged.descriptor.height} px` : ''}
              </p>
              {staged.warnings.length > 0 ? (
                <ul role="status" className="flex flex-col gap-1 text-xs text-warning">
                  {staged.warnings.map(warning => (
                    <li key={warning} className="inline-flex items-start gap-1">
                      <TriangleAlert aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0" />
                      <span>{warning}</span>
                    </li>
                  ))}
                </ul>
              ) : null}
              <div className="flex flex-wrap gap-2">
                <Button variant="secondary" size="sm" disabled={disabled || busy} onClick={() => inputRef.current?.click()}>Replace</Button>
                <Button variant="ghost" size="sm" disabled={disabled || busy} onClick={() => { setError(null); onClear(); }}>Remove</Button>
              </div>
            </div>
          </div>
        ) : (
          <button
            type="button"
            disabled={disabled || busy}
            aria-describedby={error ? errorId : hintId}
            onClick={() => inputRef.current?.click()}
            className={cn('flex min-h-56 w-full flex-col items-center justify-center gap-3 rounded-md text-center', disabled ? 'cursor-not-allowed' : 'hover:bg-surface-hover')}
          >
            {busy
              ? <LoaderCircle aria-hidden="true" className="size-10 animate-spin text-text-faint" />
              : <ImagePlus aria-hidden="true" className={cn('size-10', dragOver ? 'text-accent' : 'text-text-faint')} />}
            <span className={cn('text-md font-bold', dragOver ? 'text-accent' : 'text-text')}>{busy ? 'Loading...' : headline}</span>
            <span id={hintId} className="text-xs text-text-faint">
              {remembered
                ? `Pick ${remembered.name} (${formatMiB(remembered.size)}) again. Images aren't kept between visits.`
                : 'or drag and drop it here'}
            </span>
            <span className={cn('inline-flex h-control-height-sm items-center rounded-sm px-3 text-sm font-bold', disabled ? 'bg-accent-soft-strong text-text-disabled' : 'bg-accent text-on-accent')}>Select file</span>
          </button>
        )}
        {error ? (
          <p id={errorId} role="alert" className="mt-3 inline-flex items-start gap-1 text-xs text-danger">
            <CircleAlert aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0" />
            <span>{error}</span>
          </p>
        ) : null}
      </div>
      {footer}
    </div>
  );
}

'use client';

import Image, { type ImageProps } from 'next/image';
import React, { useState, useEffect } from 'react';

const PLACEHOLDER = '/shadcn.png';

type SharedProps = {
  /** URL or null/undefined → renders the placeholder. */
  src: string | null | undefined;
  /** Required for accessibility; may be empty for purely decorative images. */
  alt: string;
  /** Optional fallback URL to show if the primary src 404s. Defaults to /shadcn.png. */
  fallbackSrc?: string;
  /** Pass-through className applied to the rendered <Image>. */
  className?: string;
  /** When true, uses next/image priority hint (above-the-fold). */
  priority?: boolean;
  /** Quality knob; defaults to next/image's default of 75. */
  quality?: number;
  /** Sizes attribute. Required when using `fill` for proper srcset selection. */
  sizes?: string;
  /** Force unoptimized rendering (e.g. for SVG placeholders). */
  unoptimized?: boolean;
  /** style override (rare). */
  style?: React.CSSProperties;
};

type FillProps = SharedProps & {
  fill: true;
  width?: never;
  height?: never;
};

type SizedProps = SharedProps & {
  fill?: false | undefined;
  width: number;
  height: number;
};

export type TokenImageProps = FillProps | SizedProps;

/**
 * Drop-in replacement for `<img>` tags rendering token / pool images.
 *
 * Wraps `next/image` so we get:
 *   - automatic AVIF/WebP transcoding
 *   - device-pixel-ratio aware srcset
 *   - lazy loading by default (override with `priority` for above-fold)
 *   - cached at the Next image optimizer
 *
 * Behavior:
 *   - `src` is null/undefined → renders the placeholder
 *   - `src` 404s or errors → swaps to `fallbackSrc` (default `/shadcn.png`)
 *   - both fail → keeps the placeholder rather than a broken icon
 *
 * Usage — fixed-size container (most common):
 *   <div className="w-10 h-10 overflow-hidden rounded-full">
 *     <TokenImage fill src={logo} alt={symbol ?? ''} sizes="40px" />
 *   </div>
 *
 * Usage — explicit dimensions (no parent box):
 *   <TokenImage src={logo} alt={symbol ?? ''} width={40} height={40} />
 *
 * Remote hosts must be allowed in `next.config.ts` `images.remotePatterns`.
 */
export default function TokenImage(props: TokenImageProps) {
  const {
    src,
    alt,
    fallbackSrc = PLACEHOLDER,
    className,
    priority,
    quality,
    sizes,
    unoptimized,
    style,
  } = props;

  let remoteSvg = false;
  if (src && !src.startsWith('/')) {
    try { remoteSvg = new URL(src).pathname.toLowerCase().endsWith('.svg'); } catch { /* Image error uses the fallback. */ }
  }
  const requestedSrc = remoteSvg ? fallbackSrc : src || fallbackSrc;
  const [errored, setErrored] = useState(false);

  // When the parent passes a NEW src, retry — clear the prior error.
  useEffect(() => {
    setErrored(false);
  }, [requestedSrc]);

  const resolvedSrc = errored ? fallbackSrc : requestedSrc;
  const handleError = () => setErrored(true);

  // Local files (`/shadcn.png`, `/icons/foo.svg`) don't go through the
  // remote-pattern check, but SVG icons should pass `unoptimized` to
  // avoid Next's optimizer rejecting them when contentSecurityPolicy
  // sandboxing is on.
  const isLocalSvg = typeof resolvedSrc === 'string' && resolvedSrc.endsWith('.svg') && resolvedSrc.startsWith('/');

  const sharedProps = {
    alt,
    className,
    priority,
    quality,
    onError: handleError,
    unoptimized: unoptimized ?? isLocalSvg,
    style,
  } satisfies Partial<ImageProps>;

  if (props.fill) {
    return (
      <Image
        {...sharedProps}
        alt={alt}
        src={resolvedSrc}
        fill
        sizes={sizes ?? '100%'}
      />
    );
  }

  return (
    <Image
      {...sharedProps}
      alt={alt}
      src={resolvedSrc}
      width={props.width}
      height={props.height}
      sizes={sizes}
    />
  );
}

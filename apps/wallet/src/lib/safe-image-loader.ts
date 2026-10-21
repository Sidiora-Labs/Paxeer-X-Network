import type { ImageLoaderProps } from 'next/image';
import { safeMediaPath } from '@/lib/security/media-policy';

export default function safeImageLoader({
  src,
  width,
  quality,
}: ImageLoaderProps): string {
  const path = safeMediaPath(src);
  const separator = path.includes('?') ? '&' : '?';
  return `${path}${separator}w=${width}&q=${quality ?? 75}`;
}

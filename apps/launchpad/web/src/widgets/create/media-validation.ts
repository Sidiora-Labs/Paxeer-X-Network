import { hash } from './creation-domain';

export const MEDIA_MAX_BYTES = 6 * 1024 * 1024;
export const MEDIA_TYPES = ['image/png', 'image/jpeg', 'image/webp'] as const;
export type MediaType = (typeof MEDIA_TYPES)[number];
export const RECOMMENDED_MIN_EDGE = 1000;

export const MEDIA_RULES = {
  format: 'PNG, JPEG or WebP',
  size: 'Max 6 MB',
  shape: 'Square (1:1) works best',
  resolution: `${RECOMMENDED_MIN_EDGE} x ${RECOMMENDED_MIN_EDGE} px or larger works best`,
  video: 'Video is coming soon',
} as const;

export interface StagedMediaDescriptor {
  name: string;
  size: number;
  type: MediaType;
  sha256: string;
  width?: number;
  height?: number;
}

export interface StagedMedia {
  file: File;
  previewUrl: string;
  descriptor: StagedMediaDescriptor;
  warnings: string[];
}

export type MediaRuleCheck = { ok: true } | { ok: false; reason: string };

export function formatMiB(bytes: number): string {
  const mib = bytes / 1048576;
  return `${mib.toFixed(mib >= 1 ? 1 : 2)} MB`;
}

export function checkMediaRules(file: { size: number; type: string }): MediaRuleCheck {
  if (!MEDIA_TYPES.includes(file.type as MediaType)) {
    const lead = file.type.startsWith('video/') ? `${MEDIA_RULES.video}.` : "This file type isn't supported.";
    return { ok: false, reason: `${lead} Use ${MEDIA_RULES.format}.` };
  }
  if (!Number.isSafeInteger(file.size) || file.size < 1) return { ok: false, reason: 'This file is empty. Pick another one.' };
  if (file.size > MEDIA_MAX_BYTES) return { ok: false, reason: `This file is too big (${formatMiB(file.size)}). ${MEDIA_RULES.size}.` };
  return { ok: true };
}

export function dimensionWarnings(dimensions: { width: number; height: number } | null): string[] {
  if (!dimensions) return ["We couldn't check this image's size. It may not look sharp."];
  const { width, height } = dimensions;
  const warnings: string[] = [];
  if (width !== height) warnings.push(`This image isn't square (${width} x ${height} px). ${MEDIA_RULES.shape}.`);
  if (Math.min(width, height) < RECOMMENDED_MIN_EDGE) warnings.push(`This image is small (${width} x ${height} px) and may look blurry. ${MEDIA_RULES.resolution}.`);
  return warnings;
}

export function blobBytes(blob: Blob): Promise<ArrayBuffer> {
  if (typeof blob.arrayBuffer === 'function') return blob.arrayBuffer();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as ArrayBuffer);
    reader.onerror = () => reject(reader.error ?? new Error("This file couldn't be read. Try another one."));
    reader.readAsArrayBuffer(blob);
  });
}

export async function sha256Hex(blob: Blob): Promise<string> {
  const bytes = new Uint8Array(await crypto.subtle.digest('SHA-256', await blobBytes(blob)));
  return hash('0x' + Array.from(bytes, b => b.toString(16).padStart(2, '0')).join(''));
}

export async function readImageDimensions(blob: Blob): Promise<{ width: number; height: number } | null> {
  if (typeof createImageBitmap === 'function') {
    try {
      const bitmap = await createImageBitmap(blob);
      const dimensions = { width: bitmap.width, height: bitmap.height };
      bitmap.close();
      return dimensions;
    } catch {
      return null;
    }
  }
  if (typeof Image === 'undefined' || typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return null;
  return new Promise(resolve => {
    const url = URL.createObjectURL(blob);
    const image = new Image();
    image.onload = () => { URL.revokeObjectURL(url); resolve({ width: image.naturalWidth, height: image.naturalHeight }); };
    image.onerror = () => { URL.revokeObjectURL(url); resolve(null); };
    image.src = url;
  });
}

const descriptorKeys = new Set(['name', 'size', 'type', 'sha256', 'width', 'height']);

export function validateStagedMediaDescriptor(value: unknown): string | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return 'Pick your image again.';
  const d = value as Record<string, unknown>;
  if (Object.keys(d).some(key => !descriptorKeys.has(key))) return 'Pick your image again.';
  if (typeof d.name !== 'string' || !d.name.length || d.name.length > 255 || /[\p{Cc}\p{Cf}]/u.test(d.name)) return 'Pick your image again.';
  if (typeof d.size !== 'number' || !Number.isSafeInteger(d.size) || d.size < 1 || d.size > MEDIA_MAX_BYTES) return `Pick an image up to 6 MB.`;
  if (typeof d.type !== 'string' || !MEDIA_TYPES.includes(d.type as MediaType)) return `Staged media must be ${MEDIA_RULES.format}.`;
  try {
    if (typeof d.sha256 !== 'string' || hash(d.sha256) !== d.sha256) return 'Pick your image again.';
  } catch {
    return 'Pick your image again.';
  }
  for (const key of ['width', 'height'] as const) {
    const edge = d[key];
    if (edge !== undefined && (typeof edge !== 'number' || !Number.isSafeInteger(edge) || edge < 1 || edge > 65535)) return 'Pick your image again.';
  }
  return null;
}

export async function stageMedia(file: File): Promise<StagedMedia> {
  const rule = checkMediaRules(file);
  if (!rule.ok) throw new Error(rule.reason);
  const [sha256, dimensions] = await Promise.all([sha256Hex(file), readImageDimensions(file)]);
  const descriptor: StagedMediaDescriptor = {
    name: (file.name || 'image').slice(0, 255),
    size: file.size,
    type: file.type as MediaType,
    sha256,
    ...(dimensions ? { width: dimensions.width, height: dimensions.height } : {}),
  };
  const problem = validateStagedMediaDescriptor(descriptor);
  if (problem) throw new Error(problem);
  return { file, previewUrl: URL.createObjectURL(file), descriptor, warnings: dimensionWarnings(dimensions) };
}

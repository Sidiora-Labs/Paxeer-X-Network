export const TEXT_CONTRAST = 4.5;
export const LARGE_TEXT_CONTRAST = 3;
export const CONTROL_CONTRAST = 3;

const HEX = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i;
const RGBA = /^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*(?:,\s*(0|1|0?\.\d+)\s*)?\)$/;
const RGB_SPACE = /^rgb\(\s*(\d{1,3})\s+(\d{1,3})\s+(\d{1,3})\s*(?:\/\s*(0|1|0?\.\d+)\s*)?\)$/;

export interface Rgba {
    r: number;
    g: number;
    b: number;
    a: number;
}

export function parseColor(value: string): Rgba {
    const hex = HEX.exec(value);
    if (hex) {
        const digits = hex[1].length === 3 ? hex[1].replace(/./g, (digit) => digit + digit) : hex[1];
        return {
            r: parseInt(digits.slice(0, 2), 16),
            g: parseInt(digits.slice(2, 4), 16),
            b: parseInt(digits.slice(4, 6), 16),
            a: 1,
        };
    }
    const functional = RGBA.exec(value) ?? RGB_SPACE.exec(value);
    if (functional) {
        const channels = [functional[1], functional[2], functional[3]].map(Number);
        if (channels.some((channel) => channel > 255)) throw new Error(`colour channel out of range: ${value}`);
        return { r: channels[0], g: channels[1], b: channels[2], a: functional[4] === undefined ? 1 : Number(functional[4]) };
    }
    throw new Error(`not a colour value: ${value}`);
}

function channel(value: number): number {
    const srgb = value / 255;
    return srgb <= 0.04045 ? srgb / 12.92 : ((srgb + 0.055) / 1.055) ** 2.4;
}

export function relativeLuminance(value: string): number {
    const color = parseColor(value);
    if (color.a !== 1) throw new Error(`luminance needs an opaque colour: ${value}`);
    return 0.2126 * channel(color.r) + 0.7152 * channel(color.g) + 0.0722 * channel(color.b);
}

export function contrastRatio(foreground: string, background: string): number {
    const a = relativeLuminance(foreground);
    const b = relativeLuminance(background);
    return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

export function bestInk(background: string, candidates: readonly string[]): string {
    if (candidates.length === 0) throw new Error('no ink candidates');
    return candidates.reduce((best, candidate) =>
        contrastRatio(candidate, background) > contrastRatio(best, background) ? candidate : best,
    );
}

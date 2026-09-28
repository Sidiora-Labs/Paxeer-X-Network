import { readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';

const SRC = path.resolve(__dirname, '..');
const EXEMPT = [path.join(SRC, 'theme') + path.sep, path.join(SRC, 'app', 'globals.css')];
const SCANNED = /\.(?:tsx?|jsx?|mjs|cjs|css|scss)$/;
const HEX = /(^|[^&\w])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![\w-])/g;
const FUNCTIONAL = /\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\(/g;

function files(directory: string): string[] {
    return readdirSync(directory).flatMap((entry) => {
        const full = path.join(directory, entry);
        if (statSync(full).isDirectory()) return files(full);
        return SCANNED.test(entry) ? [full] : [];
    });
}

export function colourLiterals(source: string): string[] {
    const found: string[] = [];
    source.split('\n').forEach((line, index) => {
        for (const match of line.matchAll(HEX)) found.push(`${index + 1}: ${match[0].slice(match[1].length)}`);
        for (const match of line.matchAll(FUNCTIONAL)) found.push(`${index + 1}: ${match[0]}`);
    });
    return found;
}

describe('hard-coded colour literals', () => {
    it('recognises hex and functional colour notations and nothing else', () => {
        expect(colourLiterals("color: '#fff'; b: '#1c1c1a'; c: '#11223344'")).toHaveLength(3);
        expect(colourLiterals("stroke: 'rgba(255,255,255,0.15)'")).toEqual(['1: rgba(']);
        expect(colourLiterals('background: hsl(210 50% 40%); x: oklch(0.7 0.1 200)')).toHaveLength(2);
        expect(colourLiterals("fill: 'url(#premiumGrad)'; href: '#section-2'; entity: '&#123;'")).toEqual([]);
        expect(colourLiterals("label: 'Account #12'; ref: '#abcde'")).toEqual([]);
    });

    it('finds none under src outside the theme module and globals.css', () => {
        const scanned = files(SRC).filter((file) => !EXEMPT.some((exempt) => file === exempt || file.startsWith(exempt)));
        expect(scanned.length).toBeGreaterThan(100);
        const violations = scanned.flatMap((file) =>
            colourLiterals(readFileSync(file, 'utf8')).map((hit) => `${path.relative(SRC, file)}:${hit}`),
        );
        expect(violations).toEqual([]);
    });
});

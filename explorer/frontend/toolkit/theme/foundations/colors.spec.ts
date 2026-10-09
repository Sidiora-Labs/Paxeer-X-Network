import { describe, expect, it } from 'vitest';

import colors from './colors';

const DATAVIS_HUES = [ 'blue', 'green', 'grey', 'pink', 'purple', 'red', 'yellow' ];
const DATAVIS_STEPS = [ 'lowest', 'low', 'mid', 'high', 'highest' ];
const GRAY_STEPS = [ '50', '100', '200', '300', '400', '500', '600', '700', '800', '900' ];

function luminance(hex: string): number {
  const [ red, green, blue ] = [ 1, 3, 5 ].map((start) => parseInt(hex.slice(start, start + 2), 16));

  return (0.2126 * red) + (0.7152 * green) + (0.0722 * blue);
}

describe('base palette', () => {
  it('carries every data visualisation hue in five steps', () => {
    DATAVIS_HUES.forEach((hue) => {
      expect(Object.keys(colors.datavis[hue as keyof typeof colors.datavis])).toEqual(DATAVIS_STEPS);
    });
  });

  it('carries the reference blue ramp the accent is built from', () => {
    expect(colors.datavis.blue).toEqual({
      lowest: { value: '#DCF1FF' },
      low: { value: '#9DD2FF' },
      mid: { value: '#3C90FF' },
      high: { value: '#2B4FDA' },
      highest: { value: '#1E2867' },
    });
  });

  it('darkens the neutral gray ramp step by step from 50 to 900', () => {
    const shades = GRAY_STEPS.map((step) => luminance(colors.gray[step as keyof typeof colors.gray].value));

    shades.slice(1).forEach((shade, index) => {
      expect(shade).toBeLessThan(shades[index]);
    });
    expect(colors.gray['50'].value).toBe('#F8F9FC');
    expect(colors.gray['900'].value).toBe('#18191D');
  });

  it('builds black and its alpha ramp on the on-surface tone', () => {
    expect(colors.black.value).toBe('#121317');
    expect(colors.blackAlpha['500'].value).toBe('RGBA(33, 34, 38, 0.36)');
  });
});

import { describe, expect, it } from 'vitest';

import { textStyles } from './typography';

interface TextStyle {
  fontSize: string;
  lineHeight: string;
  fontWeight: string;
  fontFamily: string;
}

function style(group: 'heading' | 'text', size: string): TextStyle {
  const groupStyles = (textStyles as unknown as Record<string, Record<string, { value: TextStyle }>>)[group];

  return groupStyles[size].value;
}

const HEADING_SIZES = [ 'xl', 'lg', 'md', 'sm', 'xs' ];
const TEXT_SIZES = [ 'xl', 'md', 'sm', 'xs' ];

describe('text styles', () => {
  it('sets every heading at weight 500 on the heading typeface', () => {
    HEADING_SIZES.forEach((size) => {
      expect(style('heading', size)).toMatchObject({ fontWeight: '500', fontFamily: 'heading' });
    });
  });

  it('sets every body style at weight 400 on the body typeface', () => {
    TEXT_SIZES.forEach((size) => {
      expect(style('text', size)).toMatchObject({ fontWeight: '400', fontFamily: 'body' });
    });
  });

  it('keeps the 16, 20 and 24 pixel line boxes the pills and rows are sized on', () => {
    expect(style('text', 'xs').lineHeight).toBe('16px');
    expect(style('text', 'sm').lineHeight).toBe('20px');
    expect(style('text', 'md').lineHeight).toBe('24px');
  });

  it('never sets a line box smaller than its type', () => {
    [ ...HEADING_SIZES.map((size) => style('heading', size)), ...TEXT_SIZES.map((size) => style('text', size)) ]
      .forEach(({ fontSize, lineHeight }) => {
        expect(parseInt(lineHeight, 10)).toBeGreaterThanOrEqual(parseInt(fontSize, 10));
      });
  });
});

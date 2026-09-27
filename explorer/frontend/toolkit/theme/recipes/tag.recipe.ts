import { defineSlotRecipe } from '@chakra-ui/react';

import type { PillMetrics } from './pillSizing';
import { pillMinHeight, pillPaddingY } from './pillSizing';

// Every chip reserves room for a one pixel border on each edge, so the outlined
// chips keep the same height as the filled ones and no label outgrows its surface.
export const TAG_PILL_METRICS: Record<'md' | 'lg', PillMetrics> = {
  md: { textStyle: 'sm', paddingY: '0.5', borderWidth: 1 },
  lg: { textStyle: 'sm', paddingY: '1.5', borderWidth: 1 },
};

export const recipe = defineSlotRecipe({
  slots: [ 'root', 'label', 'startElement', 'endElement', 'closeTrigger' ],
  base: {
    root: {
      display: 'inline-flex',
      alignItems: 'center',
      verticalAlign: 'top',
      maxWidth: '100%',
      minWidth: 'auto',
      height: 'auto',
      overflow: 'hidden',
      userSelect: 'text',
      borderRadius: 'sm',
      focusVisibleRing: 'outside',
      _loading: {
        borderRadius: 'sm',
      },
      _disabled: {
        opacity: 'control.disabled',
        pointerEvents: 'none',
        cursor: 'not-allowed',
      },
    },
    label: {
      display: 'block',
      minWidth: 0,
      overflow: 'hidden',
      whiteSpace: 'nowrap',
      textOverflow: 'ellipsis',
      fontWeight: 'medium',
    },
    closeTrigger: {
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      outline: '0',
      borderRadius: 'none',
      color: 'closeButton.fg',
      focusVisibleRing: 'inside',
      focusRingWidth: '2px',
      _hover: {
        color: 'hover',
      },
    },
    startElement: {
      flexShrink: 0,
      display: 'inline-flex',
      alignItems: 'center',
      justifyContent: 'center',
      boxSize: 'var(--tag-element-size)',
      ms: 'var(--tag-element-offset)',
      '&:has([data-scope=avatar])': {
        boxSize: 'var(--tag-avatar-size)',
        ms: 'calc(var(--tag-element-offset) * 1.5)',
      },
      _icon: { boxSize: '100%' },
    },
    endElement: {
      flexShrink: 0,
      display: 'inline-flex',
      alignItems: 'center',
      justifyContent: 'center',
      boxSize: 'var(--tag-element-size)',
      me: 'var(--tag-element-offset)',
      _icon: { boxSize: '100%' },
      '&:has(button)': {
        ms: 'calc(var(--tag-element-offset) * -1)',
      },
    },
  },

  variants: {
    size: {
      md: {
        root: {
          px: '1.5',
          py: pillPaddingY(TAG_PILL_METRICS.md),
          minH: pillMinHeight(TAG_PILL_METRICS.md),
          gap: '1',
          '--tag-avatar-size': 'spacing.4',
          '--tag-element-size': 'spacing.3',
          '--tag-element-offset': '0px',
        },
        label: {
          textStyle: 'sm',
        },
      },
      lg: {
        root: {
          px: '2',
          py: pillPaddingY(TAG_PILL_METRICS.lg),
          minH: pillMinHeight(TAG_PILL_METRICS.lg),
          minW: '8',
          gap: '1',
          '--tag-avatar-size': 'spacing.4',
          '--tag-element-size': 'spacing.3',
          '--tag-element-offset': '0px',
        },
        label: {
          textStyle: 'sm',
        },
      },
    },

    variant: {
      subtle: {
        root: {
          bgColor: 'tag.root.subtle.bg',
          color: 'tag.root.subtle.fg',
        },
      },
      outlined: {
        root: {
          bgColor: 'transparent',
          color: 'text.secondary',
          borderWidth: '1px',
          borderStyle: 'solid',
          borderColor: 'border.divider',
          borderRadius: 'sm',
          _hover: {
            borderColor: 'border.strong',
          },
        },
      },
      clickable: {
        root: {
          cursor: 'pointer',
          bgColor: 'tag.root.clickable.bg',
          color: 'tag.root.clickable.fg',
          _hover: {
            opacity: 0.76,
          },
        },
      },
      filter: {
        root: {
          bgColor: 'tag.root.filter.bg',
        },
      },
      select: {
        root: {
          cursor: 'pointer',
          bgColor: 'tag.root.select.bg',
          color: 'tag.root.select.fg',
          _hover: {
            color: 'hover',
            opacity: 0.76,
          },
          _selected: {
            bgColor: 'selected.option.bg',
            color: 'whiteAlpha.800',
            _hover: {
              color: 'whiteAlpha.800',
              opacity: 0.76,
            },
          },
        },
      },
    },
  },

  defaultVariants: {
    size: 'md',
    variant: 'subtle',
  },
});

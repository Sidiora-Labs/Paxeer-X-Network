import type { Config } from 'tailwindcss';

const config: Config = {
  content: ['./src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        pax: {
          bg: 'var(--color-surface-base)',
          surface: 'var(--color-surface-raised)',
          card: 'var(--color-surface-card)',
          accent: 'var(--color-action-primary)',
          'accent-dim': 'var(--color-action-primary-muted)',
          'mid-dark': 'var(--color-text-disabled)',
          mid: 'var(--color-text-tertiary)',
          subtle: 'var(--color-text-secondary)',
          light: 'var(--color-text-primary)',
          'off-white': 'var(--color-text-strong)',
          muted: 'var(--color-text-secondary)',
          'subtle-bg': 'var(--color-surface-control)',
          success: 'var(--color-status-success)',
          error: 'var(--color-status-danger)',
          warning: 'var(--color-status-warning)',
          info: 'var(--color-status-info)',
          overlay: 'var(--color-surface-overlay)',
          control: 'var(--color-surface-control)',
          'on-accent': 'var(--color-action-on-primary)',
          line: 'var(--color-border-subtle)',
          'line-strong': 'var(--color-border-strong)',
          scrim: 'var(--color-overlay-scrim)',
          glass: 'var(--color-overlay-glass)',
        },
      },
      fontFamily: {
        sans: ['var(--font-sans)'],
        display: ['var(--font-display)'],
        mono: ['var(--font-mono)'],
      },
      borderRadius: {
        '2xl': 'var(--radius-lg)',
        '3xl': 'var(--radius-xl)',
      },
      transitionDuration: {
        DEFAULT: 'calc(150ms * var(--motion-scale))',
        fast: 'var(--motion-fast)',
        normal: 'var(--motion-normal)',
        slow: 'var(--motion-slow)',
      },
      transitionTimingFunction: {
        pax: 'var(--pax-ease)',
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.35s cubic-bezier(0.16,1,0.3,1)',
        'scale-in': 'scaleIn 0.2s ease-out',
      },
      keyframes: {
        fadeIn: {
          from: { opacity: '0' },
          to: { opacity: '1' },
        },
        slideUp: {
          from: { transform: 'translateY(100%)' },
          to: { transform: 'translateY(0)' },
        },
        scaleIn: {
          from: { transform: 'scale(0.95)', opacity: '0' },
          to: { transform: 'scale(1)', opacity: '1' },
        },
      },
    },
  },
  plugins: [],
};

export default config;

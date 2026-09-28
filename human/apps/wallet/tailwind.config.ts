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
        },
      },
      fontFamily: {
        sans: [
          'Paxeer Sans Rounded',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'sans-serif',
        ],
        display: [
          'Paxeer Sans Rounded',
          'sans-serif',
        ],
        mono: [
          'JetBrains Mono',
          'ui-monospace',
          'monospace',
        ],
      },
      borderRadius: {
        '2xl': 'var(--radius-lg)',
        '3xl': 'var(--radius-xl)',
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

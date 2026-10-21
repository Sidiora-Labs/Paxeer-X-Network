import nextVitals from 'eslint-config-next/core-web-vitals';
import prettier from 'eslint-config-prettier';

const config = [
  {
    ignores: [
      '.next/**',
      'node_modules/**',
      'out/**',
      'dist/**',
      'build/**',
      'coverage/**',
      'public/sw.js',
      'android/**',
      'public/lib/**',
    ],
  },
  ...nextVitals.map((entry) => ({
    ...entry,
    files: entry.files ?? ['**/*.{js,jsx,mjs,ts,tsx,mts,cts}'],
    ignores: [
      ...(entry.ignores ?? []),
      '.next/**',
      'node_modules/**',
      'out/**',
      'dist/**',
      'coverage/**',
      'public/sw.js',
    ],
    rules: {
      ...(entry.rules ?? {}),
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/refs': 'off',
      'react-hooks/purity': 'off',
      'react-hooks/preserve-manual-memoization': 'off',
      'react/no-unescaped-entities': 'off',
    },
  })),
  {
    files: ['src/domains/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              group: [
                '@/app/**',
                '@/components/**',
                '@/hooks/**',
                '@/providers/**',
                '@/server/**',
                '@/widgets/**',
                '@/lib/wallet/v2/**',
              ],
              message: 'Domain contracts cannot depend on application or implementation layers.',
            },
          ],
        },
      ],
    },
  },
  {
    files: ['src/components/**/*.{ts,tsx}', 'src/widgets/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              group: ['@/lib/wallet/v2/**', '@/server/**'],
              message: 'UI code must use public custody or server-edge contracts.',
            },
          ],
        },
      ],
    },
  },
  prettier,
];

export default config;

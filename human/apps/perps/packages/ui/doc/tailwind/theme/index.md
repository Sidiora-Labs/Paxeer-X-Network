# tailwind/theme — Directory Index

## Directory Responsibility

Theme CSS variables: default dark, purple dark, and light themes exported as DARK_THEME_CSS_VARS, PURPLE_DARK_THEME_CSS_VARS, LIGHT_THEME_CSS_VARS, and ThemeCssVars type.

## Files

| File               | Language   | Summary                              | Link                                     |
| ------------------ | ---------- | ------------------------------------ | ---------------------------------------- |
| index.ts           | TypeScript | Re-exports theme vars and type       | [index.md](index.md)                     |
| darkTheme.ts       | TypeScript | Default (ODS v2) dark theme CSS vars | [darkTheme.md](darkTheme.md)             |
| purpleDarkTheme.ts | TypeScript | Purple dark theme CSS variables      | [purpleDarkTheme.md](purpleDarkTheme.md) |
| lightTheme.ts      | TypeScript | Light theme CSS variables            | [lightTheme.md](lightTheme.md)           |

## Key Entities

| Entity                     | File               | Role                    |
| -------------------------- | ------------------ | ----------------------- |
| DARK_THEME_CSS_VARS        | darkTheme.ts       | Default dark theme vars |
| PURPLE_DARK_THEME_CSS_VARS | purpleDarkTheme.ts | Purple dark theme vars  |
| LIGHT_THEME_CSS_VARS       | lightTheme.ts      | Light theme vars        |
| ThemeCssVars               | index.ts           | Type for theme CSS vars |

## Theme Design Constraints

Rules every theme (and consumer preset) must follow. Distilled from the ODS v2 design spec:

- All colors flow through `--oui-*` semantic variables — no screen-local hex overrides, no duplicate theme providers.
- In `DARK_THEME_CSS_VARS`, every gradient slot has identical start/end values so gradient-driven treatments render as solid fills. Keep the pairs identical unless a theme intentionally defines real gradients (see the classic preset).
- `*-contrast` tokens are declared explicitly per theme (not derived). When changing a surface color such as `--oui-color-primary-darken` in `darkTheme.ts`, update the matching contrast token so text stays WCAG-readable.
- Branded dark surfaces (e.g. Markets metric cards, Affiliate step cards) reuse `primary-darken`/`primary-contrast` instead of introducing new tokens.
- Dark-only purple surface values must not be applied to the light theme; light brand text uses the light theme's own tokens.
- Status and trading colors (`success`, `danger`, `warning`, profit/loss) keep their established meanings — never used as generic decoration; brand purple is reserved for actions, links, and selected states, not body text.

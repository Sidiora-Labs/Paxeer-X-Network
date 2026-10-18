# darkTheme.ts

## darkTheme.ts 的职责

Default (ODS v2) dark theme CSS variables (`DARK_THEME_CSS_VARS`), `ThemeCssVars` type, and `darkThemePlugin` that injects vars on `:root`.

## darkTheme Example

```ts
import { DARK_THEME_CSS_VARS } from "@paxeer-network/ui";
```

## Font note

`--oui-font-family` defaults to the Manrope type stack (`"Manrope", "PingFang SC", "Noto Sans CJK SC", "Noto Sans", sans-serif`). The SDK does not bundle or load font files — consumers must load Manrope themselves (e.g. via `@font-face`); otherwise text silently falls back to `PingFang SC` / `Noto Sans` / system sans-serif.

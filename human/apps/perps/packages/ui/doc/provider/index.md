# provider — Directory Index

## Directory Responsibility

Theme and component providers: PaxeerNetworkThemeProvider, usePaxeerNetworkTheme, PaxeerNetworkThemeContext (ThemeConfig), and ComponentProvider for app-wide theme and component overrides.

## Files

| File                     | Language | Summary                         | Link                                               |
| ------------------------ | -------- | ------------------------------- | -------------------------------------------------- |
| componentProvider.tsx    | TSX      | ComponentProvider for overrides | [componentProvider.md](componentProvider.md)       |
| paxeerNetworkThemeContext.tsx  | TSX      | usePaxeerNetworkTheme, ThemeConfig    | [paxeerNetworkThemeContext.md](paxeerNetworkThemeContext.md)   |
| paxeerNetworkThemeProvider.tsx | TSX      | PaxeerNetworkThemeProvider            | [paxeerNetworkThemeProvider.md](paxeerNetworkThemeProvider.md) |

## Key Entities

| Entity               | File                     | Role                                    |
| -------------------- | ------------------------ | --------------------------------------- |
| PaxeerNetworkThemeProvider | paxeerNetworkThemeProvider.tsx | Wraps app with theme (CSS vars, config) |
| usePaxeerNetworkTheme      | paxeerNetworkThemeContext.tsx  | Hook for theme config                   |
| ComponentProvider    | componentProvider.tsx    | Override default components             |

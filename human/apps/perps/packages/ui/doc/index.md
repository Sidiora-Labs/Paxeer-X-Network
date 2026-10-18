# packages/ui/src — Documentation Index

## Module Overview

The `packages/ui/src` directory is the source root of the **@paxeer-network/ui** package (version 2.10.2). It provides a shared React UI component library and design tokens for paxeer-network applications, including layout primitives, form controls, overlays (modal, dialog, sheet), data display (table, typography), and theming (PaxeerNetworkThemeProvider, tailwind themes).

## Module Responsibilities

| Responsibility      | Description                                                                         |
| ------------------- | ----------------------------------------------------------------------------------- |
| Component library   | Button, Box, Flex, Grid, Input, Select, Checkbox, Switch, Badge, Avatar, Card, etc. |
| Overlays & feedback | Modal, Dialog, Sheet, Toast, Tooltip, Popover, Dropdown                             |
| Data display        | Table (DataTable), Typography, Pagination, ListView, Empty, Status                  |
| Theming & layout    | PaxeerNetworkThemeProvider, tailwind variants (theme, size, gradient), layout helpers     |
| Extension system    | Plugin registry, extension builder, deposit plugin slot                             |
| Utilities           | Hooks (useScreen, useLongPress, useThemeAttribute), formatters, tv/cn, locale       |

## Key Entities and Entry Points

| Entity                              | Type       | Role                                 | Entry                                |
| ----------------------------------- | ---------- | ------------------------------------ | ------------------------------------ |
| index.ts                            | barrel     | Main package exports                 | [entry.md](entry.md) (package entry) |
| PaxeerNetworkThemeProvider                | component  | Theme context and CSS variables      | provider/paxeerNetworkThemeProvider        |
| Button / Box / Input / Select       | components | Core UI primitives                   | button/, box/, input/, select/       |
| Modal / Dialog / Sheet              | components | Overlay containers                   | modal/, dialog/, sheet/              |
| DataTable                           | component  | Table with sort, pagination, pinning | table/                               |
| plugin (installExtension, registry) | API        | UI extension registration            | plugin/                              |
| tv / cn                             | utilities  | Styling (tailwind-variants)          | utils/tv, cn from tailwind-variants  |

## Subdirectories

| Directory                                   | Description                                                                   | Index                                                |
| ------------------------------------------- | ----------------------------------------------------------------------------- | ---------------------------------------------------- |
| [avatar](avatar/index.md)                   | Avatar and EVMAvatar components                                               | [avatar/index.md](avatar/index.md)                   |
| [badge](badge/index.md)                     | Badge component                                                               | [badge/index.md](badge/index.md)                     |
| [box](box/index.md)                         | Box layout primitive and boxVariants                                          | [box/index.md](box/index.md)                         |
| [button](button/index.md)                   | Button, IconButton, ThrottledButton, base                                     | [button/index.md](button/index.md)                   |
| [card](card/index.md)                       | Card, CardBase, HoverCard                                                     | [card/index.md](card/index.md)                       |
| [carousel](carousel/index.md)               | Carousel component (embla-carousel)                                           | [carousel/index.md](carousel/index.md)               |
| [checkbox](checkbox/index.md)               | Checkbox component                                                            | [checkbox/index.md](checkbox/index.md)               |
| [collapsible](collapsible/index.md)         | Collapsible, Collapse, Panel                                                  | [collapsible/index.md](collapsible/index.md)         |
| [dialog](dialog/index.md)                   | Dialog, AlertDialog, SimpleDialog, TriggerDialog                              | [dialog/index.md](dialog/index.md)                   |
| [divider](divider/index.md)                 | Divider component                                                             | [divider/index.md](divider/index.md)                 |
| [dropdown](dropdown/index.md)               | Dropdown, MenuItem, Checkbox, RadioGroup, Tokens                              | [dropdown/index.md](dropdown/index.md)               |
| [empty](empty/index.md)                     | Empty state component                                                         | [empty/index.md](empty/index.md)                     |
| [flex](flex/index.md)                       | Flex layout component                                                         | [flex/index.md](flex/index.md)                       |
| [grid](grid/index.md)                       | Grid and Span components                                                      | [grid/index.md](grid/index.md)                       |
| [helpers](helpers/index.md)                 | parse-props, sizeType, colorType, layoutClassHelper, component-props          | [helpers/index.md](helpers/index.md)                 |
| [hooks](hooks/index.md)                     | useLongPress, useMediaQuery, useObserverElement, useScreen, useThemeAttribute | [hooks/index.md](hooks/index.md)                     |
| [icon](icon/index.md)                       | Icon components (baseIcon, baseIconWithPath, many named icons)                | [icon/index.md](icon/index.md)                       |
| [input](input/index.md)                     | Input, TextField, formatters, QuantityInput, InputWithTooltip                 | [input/index.md](input/index.md)                     |
| [layout](layout/index.md)                   | layout, shadow, position, decoration, visible, gap, intensity                 | [layout/index.md](layout/index.md)                   |
| [listView](listView/index.md)               | ListView and useEndReached                                                    | [listView/index.md](listView/index.md)               |
| [locale](locale/index.md)                   | Locale context, provider, useLocale, en                                       | [locale/index.md](locale/index.md)                   |
| [logo](logo/index.md)                       | Logo component                                                                | [logo/index.md](logo/index.md)                       |
| [marquee](marquee/index.md)                 | Marquee component                                                             | [marquee/index.md](marquee/index.md)                 |
| [misc](misc/index.md)                       | Either, Switch (conditional render)                                           | [misc/index.md](misc/index.md)                       |
| [modal](modal/index.md)                     | Modal, useModal, presets (alert, confirm, sheet, dialog)                      | [modal/index.md](modal/index.md)                     |
| [pagination](pagination/index.md)           | PaginationItems                                                               | [pagination/index.md](pagination/index.md)           |
| [pickers](pickers/index.md)                 | DatePicker, DateRangePicker, Picker                                           | [pickers/index.md](pickers/index.md)                 |
| [plugin](plugin/index.md)                   | Plugin registry, installExtension, useExtensionBuilder, deposit plugin        | [plugin/index.md](plugin/index.md)                   |
| [popover](popover/index.md)                 | Popover component                                                             | [popover/index.md](popover/index.md)                 |
| [provider](provider/index.md)               | PaxeerNetworkThemeProvider, paxeerNetworkThemeContext, componentProvider                  | [provider/index.md](provider/index.md)               |
| [radio](radio/index.md)                     | Radio component                                                               | [radio/index.md](radio/index.md)                     |
| [scrollIndicator](scrollIndicator/index.md) | ScrollIndicator, ScrollButton, useScroll, useDrag                             | [scrollIndicator/index.md](scrollIndicator/index.md) |
| [scrollarea](scrollarea/index.md)           | ScrollArea component                                                          | [scrollarea/index.md](scrollarea/index.md)           |
| [select](select/index.md)                   | Select, SelectItem, SelectOption, chains, tokens, withOptions                 | [select/index.md](select/index.md)                   |
| [sheet](sheet/index.md)                     | Sheet, SimpleSheet, ActionSheet                                               | [sheet/index.md](sheet/index.md)                     |
| [slider](slider/index.md)                   | Slider component and utils                                                    | [slider/index.md](slider/index.md)                   |
| [spinner](spinner/index.md)                 | Spinner component                                                             | [spinner/index.md](spinner/index.md)                 |
| [status](status/index.md)                   | DotStatus component                                                           | [status/index.md](status/index.md)                   |
| [switch](switch/index.md)                   | Switch component                                                              | [switch/index.md](switch/index.md)                   |
| [table](table/index.md)                     | DataTable, hooks, features, table cells/header/body/pagination                | [table/index.md](table/index.md)                     |
| [tabs](tabs/index.md)                       | Tabs, TabsBase                                                                | [tabs/index.md](tabs/index.md)                       |
| [tag](tag/index.md)                         | Tag component                                                                 | [tag/index.md](tag/index.md)                         |
| [tailwind](tailwind/index.md)               | OUITailwind: base, size, position, gradient, theme, scrollBar, chart          | [tailwind/index.md](tailwind/index.md)               |
| [theme](theme/index.md)                     | theme.ts (theme utilities)                                                    | [theme/index.md](theme/index.md)                     |
| [tips](tips/index.md)                       | Tips component                                                                | [tips/index.md](tips/index.md)                       |
| [toast](toast/index.md)                     | Toaster, toastTile, toast API                                                 | [toast/index.md](toast/index.md)                     |
| [tooltip](tooltip/index.md)                 | Tooltip component                                                             | [tooltip/index.md](tooltip/index.md)                 |
| [transitions](transitions/index.md)         | Fade transition                                                               | [transitions/index.md](transitions/index.md)         |
| [typography](typography/index.md)           | Text, Numeral, Formatted, Gradient, Symbol, Statistic                         | [typography/index.md](typography/index.md)           |
| [utils](utils/index.md)                     | tv, string, transition, cn re-export                                          | [utils/index.md](utils/index.md)                     |

## Top-Level Files

| File        | Language   | Role                                                                     | Link                     |
| ----------- | ---------- | ------------------------------------------------------------------------ | ------------------------ |
| index.ts    | TypeScript | Package barrel; exports all public components and types                  | [entry.md](entry.md)     |
| install.tsx | TSX        | Placeholder install (no-op export); reserved for extension registration  | [install.md](install.md) |
| version.ts  | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` for @paxeer-network/ui | [version.md](version.md) |

## Search Keywords / Aliases

- paxeer-network ui, @paxeer-network/ui, UI package, component library
- Button, Box, Input, Select, Modal, Dialog, Table, Theme
- PaxeerNetworkThemeProvider, tailwind, tv, cn, plugin, extension

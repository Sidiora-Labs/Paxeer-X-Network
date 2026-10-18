# index.ts (package entry)

## index.ts 的职责

Main package barrel for `@paxeer-network/ui`. Re-exports all public components (Button, Box, Input, Select, Modal, Table, etc.), types (ButtonProps, BoxProps, InputProps, …), theme (PaxeerNetworkThemeProvider, usePaxeerNetworkTheme), plugin API, hooks, utils (tv, cn), and third-party re-exports (tailwind-variants, react-hot-toast, embla-carousel). Entry point for consuming applications.

## index.ts 对外导出内容

| Name                                                                                                     | Type                    | Role              | Description                                                                                              |
| -------------------------------------------------------------------------------------------------------- | ----------------------- | ----------------- | -------------------------------------------------------------------------------------------------------- |
| Button, Box, Grid, Flex                                                                                  | component               | Layout & actions  | From button, box, grid, flex                                                                             |
| Typography, Spinner, Input, Checkbox, Switch, Badge, Logo                                                | component               | Form & display    | From typography, spinner, input, checkbox, switch, badge, logo                                           |
| Tooltip, Table, ScrollArea, Dialog, Sheet, Divider, Tabs                                                 | component               | Overlays & layout | From tooltip, table, scrollarea, dialog, sheet, divider, tabs                                            |
| PaginationItems, Select, Popover, Card, Pickers, Slider, Toast, ListView, Collapsible, Marquee, Dropdown | component               | Data & overlay    | From pagination, select, popover, card, pickers, slider, toast, listView, collapsible, marquee, dropdown |
| Icon, Modal, EVMAvatar, Avatar                                                                           | component               | Icons & modal     | From icon, modal, avatar                                                                                 |
| PaxeerNetworkThemeProvider, usePaxeerNetworkTheme                                                                    | component / hook        | Theme             | From provider                                                                                            |
| plugin (installExtension, etc.)                                                                          | API                     | Extensions        | From plugin                                                                                              |
| Either                                                                                                   | component               | Misc              | From misc/either                                                                                         |
| cn                                                                                                       | function                | Styling           | cnBase from tailwind-variants                                                                            |
| tv                                                                                                       | function                | Styling           | From utils/tv                                                                                            |
| toast                                                                                                    | function                | Notifications     | default from react-hot-toast                                                                             |
| utils, hooks                                                                                             | namespace               | Utils & hooks     | From utils, hooks                                                                                        |
| OUITailwind, DARK_THEME_CSS_VARS, PURPLE_DARK_THEME_CSS_VARS, LIGHT_THEME_CSS_VARS, ThemeCssVars         | namespace / vars / type | Tailwind & theme  | From tailwind                                                                                            |
| ButtonProps, BoxProps, FlexProps, InputProps, TextFieldProps, …                                          | type                    | Prop types        | Various                                                                                                  |

## index.ts 依赖与调用关系

- **Upstream**: All submodules under `packages/ui/src` (button, box, input, modal, table, …).
- **Downstream**: Applications that import from `@paxeer-network/ui`.
- **Side effect**: `./install` is imported for side effects (currently no-op).

## index.ts Example

```tsx
import {
  Button,
  Box,
  Input,
  PaxeerNetworkThemeProvider,
  usePaxeerNetworkTheme,
  DataTable,
  toast,
  cn,
} from "@paxeer-network/ui";

function App() {
  return (
    <PaxeerNetworkThemeProvider>
      <Box p="4">
        <Button variant="contained" color="primary">
          Submit
        </Button>
        <Input placeholder="Amount" />
      </Box>
    </PaxeerNetworkThemeProvider>
  );
}
```

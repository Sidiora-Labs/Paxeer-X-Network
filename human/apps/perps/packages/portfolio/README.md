# @paxeer-network/portfolio Mobile Pages Usage Guide

If you are using the layout components from @paxeer-network/portfolio directly and are an existing user, when you want to use mobile pages, we have added HistoryPage and SettingPage. You can add the corresponding routes to fully utilize the mobile functionality.

## Component Import Paths

### HistoryPage Component

```typescript
import { HistoryModule } from "@paxeer-network/portfolio";

// Available components
const { HistoryPage, HistoryWidget } = HistoryModule;
```

### SettingPage Component

```typescript
import { SettingModule } from "@paxeer-network/portfolio";

// Available components
const { SettingPage, SettingWidget } = SettingModule;
```

## Route Path Configuration

Based on the `PortfolioLeftSidebarPath` enum definition, the standard route paths for mobile pages are:

```typescript
// History page path
const HISTORY_PATH = "/portfolio/history";

// Settings page path
const SETTING_PATH = "/portfolio/setting";
```

## Complete Route Configuration Examples

### React Router Configuration Example

```typescript
import { Routes, Route } from "react-router-dom";
import { HistoryModule, SettingModule } from "@paxeer-network/portfolio";

const { HistoryPage } = HistoryModule;
const { SettingPage } = SettingModule;

function App() {
  return (
    <Routes>
      {/* Other routes */}
      <Route path="/portfolio" element={<PortfolioOverview />} />
      <Route path="/portfolio/positions" element={<PositionsPage />} />
      <Route path="/portfolio/orders" element={<OrdersPage />} />

      {/* New mobile page routes */}
      <Route path="/portfolio/history" element={<HistoryPage />} />
      <Route path="/portfolio/setting" element={<SettingPage />} />
    </Routes>
  );
}
```

### Next.js App Router Configuration Example

```typescript
// app/portfolio/history/page.tsx
import { HistoryModule } from "@paxeer-network/portfolio";

const { HistoryPage } = HistoryModule;

export default function HistoryPageRoute() {
  return <HistoryPage />;
}
```

```typescript
// app/portfolio/setting/page.tsx
import { SettingModule } from "@paxeer-network/portfolio";

const { SettingPage } = SettingModule;

export default function SettingPageRoute() {
  return <SettingPage />;
}
```

### Next.js Pages Router Configuration Example

```typescript
// pages/portfolio/history.tsx
import { HistoryModule } from "@paxeer-network/portfolio";

const { HistoryPage } = HistoryModule;

export default HistoryPage;
```

```typescript
// pages/portfolio/setting.tsx
import { SettingModule } from "@paxeer-network/portfolio";

const { SettingPage } = SettingModule;

export default SettingPage;
```

## Mobile Navigation Configuration

If you are using Portfolio's layout components, mobile navigation will automatically handle these routes. Ensure your `routerAdapter` is properly configured:

```typescript
import { PortfolioLayoutWidget } from "@paxeer-network/portfolio";

const routerAdapter = {
  onRouteChange: ({ href, name }) => {
    // Handle route navigation logic
    router.push(href);
  },
  currentPath: router.pathname, // Current path
};

function PortfolioLayout({ children }) {
  return (
    <PortfolioLayoutWidget
      routerAdapter={routerAdapter}
      current={router.pathname}
    >
      {children}
    </PortfolioLayoutWidget>
  );
}
```

## Feature Description

### HistoryPage

- **Function**: Display user's trading history records
- **Includes**: Deposit/withdrawal history, funding fee history, distribution history
- **Mobile Optimization**: Automatically adapts to mobile interface, provides touch-friendly interactive experience

### SettingPage

- **Function**: User settings page
- **Includes**: Account settings, preference settings, etc.
- **Mobile Optimization**: Responsive design, supports mobile operations

## Important Notes

1. **Path Consistency**: Ensure route paths are consistent with `PortfolioLeftSidebarPath` enum values
2. **Mobile Detection**: Components automatically detect mobile environment and render corresponding UI
3. **Dependency Requirements**: Ensure all necessary dependency packages are installed, including `@paxeer-network/ui`, `@paxeer-network/i18n`, etc.
4. **RouterAdapter**: Ensure proper implementation of `routerAdapter` interface to support page navigation functionality

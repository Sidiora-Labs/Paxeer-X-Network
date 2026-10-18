# Scaffold Component Integration Guide

## Overview

Scaffold is a powerful layout component that provides a complete application scaffolding structure, including top navigation bar, sidebar, main content area, bottom navigation, and footer. This component supports responsive layouts for both desktop and mobile devices and offers rich customization options.

## Installation and Import

```typescript
import { Scaffold, type ScaffoldProps } from "@paxeer-network/ui-scaffold";
```

## Basic Usage

```typescript
import React from 'react';
import { Scaffold } from "@paxeer-network/ui-scaffold";

const App = () => {
  return (
    <Scaffold>
      <div>Your main content here</div>
    </Scaffold>
  );
};
```

## ScaffoldProps Properties Detailed

### 1. leftSidebar (Optional)

- **Type**: `React.ReactNode`
- **Description**: Custom left sidebar component. If provided, the layout will use this component instead of the default sidebar component
- **Use Case**: When you need to completely customize the sidebar style and functionality

```typescript
<Scaffold
  leftSidebar={<CustomSidebar />}
>
  {children}
</Scaffold>
```

### 2. leftSideProps (Optional)

- **Type**: `SideBarProps`
- **Description**: Configuration properties for the default sidebar component
- **Main Properties**:
  - `title`: Sidebar title
  - `items`: Array of menu items
  - `open`: Whether the sidebar is expanded
  - `onOpenChange`: Callback for expansion state changes
  - `onItemSelect`: Callback for menu item selection
  - `current`: Currently selected menu item
  - `maxWidth`: Maximum width when expanded (default: 185px)
  - `minWidth`: Minimum width when collapsed (default: 98px)

```typescript
const sidebarMenus = [
  { name: "Dashboard", href: "/dashboard", icon: <DashboardIcon /> },
  { name: "Trading", href: "/trading", icon: <TradingIcon /> },
  { name: "Portfolio", href: "/portfolio", icon: <PortfolioIcon /> }
];

<Scaffold
  leftSideProps={{
    title: "Navigation",
    items: sidebarMenus,
    current: "/dashboard",
    onItemSelect: (item) => console.log('Selected:', item),
    maxWidth: 200,
    minWidth: 80
  }}
>
  {children}
</Scaffold>
```

### 3. topBar (Optional)

- **Type**: `React.ReactNode`
- **Description**: Custom top navigation bar component
- **Use Case**: When you need to completely customize the top navigation bar

```typescript
<Scaffold
  topBar={<CustomTopBar />}
>
  {children}
</Scaffold>
```

### 4. mainNavProps (Optional)

- **Type**: `MainNavWidgetProps`
- **Description**: Configuration properties for the default main navigation component
- **Main Properties**:
  - `leading`: Content on the left side of the navigation bar
  - `trailing`: Content on the right side of the navigation bar
  - `logo`: Brand logo configuration
  - `mainMenus`: Array of main menu items
  - `campaigns`: Campaign menu item
  - `campaignPosition`: Campaign menu position
  - `initialMenu`: Initial menu path
  - `onItemClick`: Menu item click callback
  - `leftNav`: Left navigation configuration for mobile drawer (only works on mobile)
  - `customLeftNav`: Custom left navigation component to replace the default leftNav
  - `customRender`: Function to customize the main navigation layout
    - **Type**: `(components: MainNavComponents) => ReactNode`
    - **Components**:
      - `title`: Logo or title component (desktop & mobile)
      - `languageSwitcher`: Language selection component (desktop & mobile)
      - `subAccount`: Sub-account component (desktop & mobile)
      - `linkDevice`: Device linking component (desktop & mobile)
      - `chainMenu`: Chain selection menu (desktop & mobile)
      - `walletConnect`: Wallet connection component (desktop & mobile)
      - `mainNav`: Main navigation menu (desktop only)
      - `accountSummary`: Account summary component (desktop only)
      - `leftNav`: Left navigation component (mobile only)
      - `scanQRCode`: QR code scanner component (mobile only)

```typescript
<Scaffold
  mainNavProps={{
    logo: {
      src: "/logo.png",
      alt: "Company Logo"
    },
    mainMenus: [
      { name: "Trade", href: "/trade" },
      { name: "Portfolio", href: "/portfolio" }
    ],
    leading: <BrandSection />,
    trailing: <UserActions />,
    onItemClick: ({ href, name }) => {
      console.log(`Navigating to ${href}`);
    },
    leftNav: {
      menus: [
        { name: "Dashboard", href: "/dashboard", icon: <DashboardIcon /> },
        { name: "Trading", href: "/trading", icon: <TradingIcon /> },
        { name: "Portfolio", href: "/portfolio", icon: <PortfolioIcon /> }
      ],
      leading: <CustomLeftNavHeader />,
      twitterUrl: "https://twitter.com/yourhandle",
      telegramUrl: "https://t.me/yourgroup",
      discordUrl: "https://discord.gg/yourserver",
      duneUrl: "https://dune.com/youranalytics",
      // customFooter: <ReactNode>, // optional. If not provided, default will be rendered.
    },
    // Custom layout for main navigation
    customRender: (components) => {
      // Mobile layout
      if (isMobile) {
        return (
          <Flex width="100%" justify="between">
            <Flex gapX={2}>
              {components.leftNav}
              {components.title}
            </Flex>
            <Flex gapX={2}>
              {components.languageSwitcher}
              {components.scanQRCode}
              {components.linkDevice}
              {components.chainMenu}
              {components.walletConnect}
            </Flex>
          </Flex>
        );
      }

      // Desktop layout
      return (
        <Flex width="100%" justify="between">
          <Flex gapX={2}>
            {components.title}
            {components.mainNav}
          </Flex>
          <Flex gapX={2}>
            {components.accountSummary}
            {components.linkDevice}
            {components.languageSwitcher}
            {components.subAccount}
            {components.chainMenu}
            {components.walletConnect}
          </Flex>
        </Flex>
      );
    }
  }}
>
  {children}
</Scaffold>
```

### 5. leftNav Configuration (Optional)

`leftNav` is a configuration property within `mainNavProps` that controls the mobile slide-out navigation drawer. It only appears on mobile devices and provides a comprehensive menu system.

#### LeftNavProps Properties

- **Type**: `LeftNavProps`
- **Description**: Configuration for the mobile left navigation drawer
- **Main Properties**:
  - `menus`: Array of navigation menu items for the drawer
  - `leading`: Custom content at the top of the drawer (below logo)
  - `twitterUrl`: Twitter/X social media link
  - `telegramUrl`: Telegram community link
  - `discordUrl`: Discord community link
  - `duneUrl`: Dune Analytics link
  - `feedbackUrl`: Feedback form link (deprecated; does not control the leftNav drawer footer UI)
  - `customFooter`: Slot for TradingView T&C custom content in the leftNav drawer bottom (mweb). If not provided, default `Charts powered by TradingView` link will be rendered.
  - `customLeftNav`: Custom component to replace the default leftNav trigger

#### LeftNavItem Properties

Each menu item in the `menus` array supports:

- `name`: Display name of the menu item
- `href`: Navigation URL
- `target`: Link target (e.g., "\_blank" for new window). If provided, will use `window.open()` instead of router navigation (optional)
- `icon`: Icon component to display (optional)
- `trailing`: Additional content on the right side (optional)
- `customRender`: Custom render function for complete control over item appearance

#### Usage Example

```typescript
<Scaffold
  mainNavProps={{
    leftNav: {
      menus: [
        {
          name: "Dashboard",
          href: "/dashboard",
          icon: <DashboardIcon />
        },
        {
          name: "Trading",
          href: "/trading",
          icon: <TradingIcon />,
          trailing: <NotificationBadge />
        },
        {
          name: "Portfolio",
          href: "/portfolio",
          icon: <PortfolioIcon />,
          customRender: ({ name, href, isActive }) => (
            <div className={`custom-menu-item ${isActive ? 'active' : ''}`}>
              <PortfolioIcon />
              <span>{name}</span>
              <SpecialBadge />
            </div>
          )
        },
        {
          name: "External Link",
          href: "https://external-site.com",
          target: "_blank",
          icon: <ExternalLinkIcon />
        }
      ],
      leading: (
        <div className="px-3 py-2">
          <Text className="text-sm text-gray-500">Quick Actions</Text>
        </div>
      ),
      twitterUrl: "https://twitter.com/yourhandle",
      telegramUrl: "https://t.me/yourgroup",
      discordUrl: "https://discord.gg/yourserver",
      duneUrl: "https://dune.com/youranalytics",
      // customFooter: <ReactNode>, // optional. If not provided, default will be rendered.
    }
  }}
>
  {children}
</Scaffold>
```

#### Mobile Navigation Behavior

- **Trigger**: The leftNav appears as a hamburger menu icon on mobile devices
- **Drawer**: Slides out from the left side when triggered
- **Content Structure**:
  1. Logo at the top
  2. Leading content (if provided)
  3. Sub-account selector (if user is logged in with trading enabled)
  4. Menu items list (scrollable if needed)
  5. Social media links at the bottom
  6. TradingView T&C at the very bottom (defaults to `Charts powered by TradingView`, customizable via `customFooter`)
- **Auto-close**: The drawer automatically closes when a menu item is selected
- **Navigation Behavior**:
  - Items with `target` property will open in new window/tab using `window.open()`
  - Items without `target` will use router navigation and close the drawer

#### Advanced Customization

You can provide a completely custom left navigation trigger:

```typescript
<Scaffold
  mainNavProps={{
    customLeftNav: <CustomHamburgerButton />,
    leftNav: {
      // ... leftNav configuration
    }
  }}
>
  {children}
</Scaffold>
```

### 6. bottomNavProps (Optional)

- **Type**: `BottomNavProps`
- **Description**: Mobile bottom navigation configuration (only displayed on mobile devices)
- **Main Properties**:
  - `mainMenus`: Array of bottom menu items

```typescript
const bottomMenus = [
  {
    name: "Home",
    href: "/home",
    activeIcon: <HomeActiveIcon />,
    inactiveIcon: <HomeInactiveIcon />
  },
  {
    name: "Trade",
    href: "/trade",
    activeIcon: <TradeActiveIcon />,
    inactiveIcon: <TradeInactiveIcon />
  }
];

<Scaffold
  bottomNavProps={{
    mainMenus: bottomMenus,
  }}
>
  {children}
</Scaffold>
```

### 7. footer (Optional)

- **Type**: `React.ReactNode`
- **Description**: Custom footer component

```typescript
<Scaffold
  footer={<CustomFooter />}
>
  {children}
</Scaffold>
```

### 8. footerProps (Optional)

- **Type**: `FooterProps`
- **Description**: Configuration properties for the default footer component
- **Main Properties**:
  - `telegramUrl`: Telegram link
  - `twitterUrl`: Twitter link
  - `discordUrl`: Discord link
  - `trailing`: Content on the right side of the footer. If not provided, a default `Charts powered by TradingView` link will be rendered.
  - `poweredBy`: Powered-by content. Hidden by default. Use `true` to show the default `Powered by paxeer-network`, or pass a custom ReactNode.

```typescript
<Scaffold
  footerProps={{
    telegramUrl: "https://t.me/yourgroup",
    twitterUrl: "https://twitter.com/yourhandle",
    discordUrl: "https://discord.gg/yourserver",
    trailing: <AdditionalFooterContent />,
    poweredBy: true
  }}
>
  {children}
</Scaffold>
```

```typescript
<Scaffold
  footerProps={{
    poweredBy: <a href="https://example.com">Powered by My DEX</a>
  }}
>
  {children}
</Scaffold>
```

### 9. routerAdapter (Optional)

- **Type**: `RouterAdapter`
- **Description**: Router adapter for integrating with different routing libraries
- **Main Properties**:
  - `onRouteChange`: Route change handler function
  - `currentPath`: Current path

```typescript
// React Router integration example
const routerAdapter = {
  onRouteChange: (option) => {
    if (option.target === '_blank') {
      window.open(option.href, '_blank');
    } else {
      navigate(option.href);
    }
  },
  currentPath: location.pathname
};

<Scaffold
  routerAdapter={routerAdapter}
>
  {children}
</Scaffold>
```

### 10. classNames (Optional)

- **Type**: Style class name configuration object
- **Description**: Used to customize styles for various layout areas
- **Configurable Areas**:
  - `root`: Root container (topNavbar + container + footer)
  - `container`: Main container
  - `content`: Content area
  - `body`: Body area (leftSidebar + content)
  - `leftSidebar`: Left sidebar
  - `topNavbar`: Top navigation bar
  - `footer`: Footer

```typescript
<Scaffold
  classNames={{
    root: "custom-root-class",
    container: "custom-container-class",
    content: "custom-content-class",
    body: "custom-body-class",
    leftSidebar: "custom-sidebar-class",
    topNavbar: "custom-navbar-class",
    footer: "custom-footer-class"
  }}
>
  {children}
</Scaffold>
```

## Complete Usage Example

```typescript
import React from 'react';
import { Scaffold } from "@paxeer-network/ui-scaffold";
import { useNavigate, useLocation } from 'react-router-dom';

const App = () => {
  const navigate = useNavigate();
  const location = useLocation();

  const sidebarMenus = [
    {
      name: "Dashboard",
      href: "/dashboard",
      icon: <DashboardIcon />
    },
    {
      name: "Trading",
      href: "/trading",
      icon: <TradingIcon />
    },
    {
      name: "Portfolio",
      href: "/portfolio",
      icon: <PortfolioIcon />
    }
  ];

  const bottomMenus = [
    {
      name: "Home",
      href: "/home",
      activeIcon: <HomeActiveIcon />,
      inactiveIcon: <HomeInactiveIcon />
    },
    {
      name: "Trade",
      href: "/trade",
      activeIcon: <TradeActiveIcon />,
      inactiveIcon: <TradeInactiveIcon />
    }
  ];

  const routerAdapter = {
    onRouteChange: (option) => {
      if (option.target === '_blank') {
        window.open(option.href, '_blank');
      } else {
        navigate(option.href);
      }
    },
    currentPath: location.pathname
  };

  return (
    <Scaffold
      leftSideProps={{
        title: "Navigation",
        items: sidebarMenus,
        current: location.pathname,
        onItemSelect: (item) => {
          if (item.href) {
            navigate(item.href);
          }
        }
      }}
      mainNavProps={{
        logo: {
          src: "/logo.png",
          alt: "Company Logo"
        },
        mainMenus: [
          { name: "Trade", href: "/trade" },
          { name: "Portfolio", href: "/portfolio" }
        ],
        leading: <BrandSection />,
        trailing: <UserActions />,
        onItemClick: ({ href }) => navigate(href),
        leftNav: {
          menus: sidebarMenus, // Reuse the same menu items
          twitterUrl: "https://twitter.com/yourhandle",
          telegramUrl: "https://t.me/yourgroup",
          discordUrl: "https://discord.gg/yourserver",
          // customFooter: <ReactNode>
        }
      }}
      bottomNavProps={{
        mainMenus: bottomMenus,
      }}
      footerProps={{
        telegramUrl: "https://t.me/yourgroup",
        twitterUrl: "https://twitter.com/yourhandle",
        discordUrl: "https://discord.gg/yourserver"
      }}
      routerAdapter={routerAdapter}
      classNames={{
        content: "p-4",
        leftSidebar: "border-r border-gray-200"
      }}
    >
      <div className="min-h-screen">
        {/* Your main application content */}
        <h1>Welcome to your application</h1>
        <p>This is the main content area.</p>
      </div>
    </Scaffold>
  );
};

export default App;
```

## Responsive Behavior

The Scaffold component automatically detects device type and provides appropriate layouts:

- **Desktop**: Displays complete sidebar, top navigation bar, and footer
- **Mobile**: Hides sidebar, displays bottom navigation bar and left navigation drawer (leftNav), optimized for touch interaction

### Mobile Navigation Features

- **Left Navigation Drawer**: Accessed via hamburger menu icon, provides slide-out menu with complete navigation options
- **Bottom Navigation**: Fixed bottom bar for quick access to main sections
- **Responsive Top Bar**: Adapts to show essential controls and navigation elements

## Best Practices

1. **Router Integration**: Always provide `routerAdapter` to ensure navigation functionality works properly
2. **Responsive Design**: Provide `bottomNavProps` and `leftNav` configuration for optimal mobile experience
3. **Style Customization**: Use the `classNames` property for style customization instead of directly modifying component styles
4. **Menu State Management**: Ensure the `current` property is synchronized with actual route state
5. **Performance Optimization**: For large menu lists, consider using React.memo to optimize rendering performance
6. **Mobile Navigation**: Configure `leftNav` with social media links and feedback URL to provide comprehensive mobile navigation experience

## Important Notes

- The Scaffold component must be wrapped in `ScaffoldProvider` to work properly (automatically handled internally)
- Sidebar expand/collapse state is automatically saved to localStorage
- Mobile and desktop use different layout components to ensure good user experience on different devices
- The `leftNav` drawer is only available on mobile devices and provides a comprehensive navigation menu
- All navigation callbacks should properly handle route navigation logic

## Related Components

- `ScaffoldProvider`: Provides Scaffold context
- `useScaffoldContext`: Hook to get Scaffold state
- `MobileScaffold`: Mobile layout component
- `DesktopScaffold`: Desktop layout component
- `SubAccountWidget`: Sub-account selection component (exported from ui-scaffold)

# SettingsModal Component Documentation

## Overview

The `SettingsModal` is a premium animated settings drawer that provides global app-level configuration options. It features smooth framer-motion animations, visual theme previews, and organized setting categories with a native drawer-style interface.

## Architecture

### Core Structure
```
SettingsModal (Portal)
├── Backdrop (framer-motion)
└── Drawer Container (framer-motion flex)
    ├── Header (fixed)
    └── Scrollable Content
        ├── Theme Section (visual previews)
        ├── User Experience Settings
        ├── Privacy & Security Settings
        ├── Advanced Settings
        └── Footer
```

## Theme System

### Theme Options Configuration
Each theme includes visual preview properties:

```typescript
const themeOptions = [
  { 
    label: "Light", 
    icon: SunIcon, 
    value: 'light',                    // next-themes value
    gradient: 'linear-gradient(...)',  // Preview gradient
    badgeColor: '#1e293b',            // Badge text color
    badgeBg: '#f1f5f9',               // Badge background
    border: '#e2e8f0'                 // Border color
  },
  // ... more themes
];
```

### Visual Theme Preview System
- **Color Preview Circle**: 10x10px gradient showing theme colors
- **Check Mark**: Appears on selected theme with theme-appropriate colors
- **Theme Descriptions**: Contextual descriptions for each theme
- **Interactive Cards**: Hover effects with accent border highlighting

## Settings Architecture

### State Management
Individual `useState` hooks for each setting:
```typescript
const [notifications, setNotifications] = useState(true);
const [soundEffects, setSoundEffects] = useState(false);
const [privateMode, setPrivateMode] = useState(false);
// ... more settings
```

### Setting Categories

#### Appearance
- **Theme Selection**: Visual theme picker with live previews

#### User Experience
- **Push Notifications**: App-wide notification toggle
- **Sound Effects**: Audio feedback for interactions
- **Auto Refresh Data**: Automatic data polling

#### Privacy & Security
- **Private Mode**: Hide sensitive information
- **Usage Analytics**: Data collection consent

#### Advanced
- **Advanced Mode**: Enable power user features

### Adding New Settings

1. **Add State Variable**:
```typescript
const [newSetting, setNewSetting] = useState(false);
```

2. **Add Setting Row**:
```typescript
<div className="flex items-center justify-between">
  <div className="flex items-center gap-3">
    <div className="p-2 rounded-lg bg-accent/10">
      <IconName className="h-4 w-4 text-accent" />
    </div>
    <div>
      <Label htmlFor="new-setting" className="text-sm font-medium cursor-pointer">
        Setting Name
      </Label>
      <p className="text-xs text-muted-foreground">
        Setting description
      </p>
    </div>
  </div>
  <Switch 
    id="new-setting" 
    checked={newSetting}
    onCheckedChange={setNewSetting}
    className="data-[state=checked]:bg-accent"
  />
</div>
```

## Animation System

### Drawer Animations
Uses framer-motion with spring physics:
```typescript
// Direction-aware animations
initial={isMobile ? { y: "100%" } : { x: "100%" }}
animate={isMobile ? { y: 0 } : { x: 0 }}
exit={isMobile ? { y: "100%" } : { x: "100%" }}
transition={{
  type: "spring",
  damping: 30,
  stiffness: 300,
}}
```

### Animation Behavior
- **Desktop**: Slides in from right edge
- **Mobile**: Slides up from bottom (like iOS bottom sheets)
- **Backdrop**: Smooth fade in/out with blur effect
- **Spring Physics**: Natural feeling animations with bounce

## Scroll Management

### Body Scroll Prevention
```typescript
// Simple and effective approach
useEffect(() => {
  if (isOpen) {
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = '';
    };
  }
}, [isOpen]);
```

### Mobile Touch Scrolling
**Critical Implementation Details:**
- Uses `overflow-y-scroll` instead of `overflow-y-auto`
- Applies `WebkitOverflowScrolling: 'touch'` for iOS momentum
- Explicit height (`h-[85vh]`) instead of max-height
- Flex container structure for proper height calculation

**Why This Works:**
- **Simplified Structure**: Direct flex container without nested scrollable divs
- **Proper Height**: Fixed height allows overflow calculation
- **Native Touch**: Leverages browser's native touch scrolling

## Styling System

### Design Tokens
- **Colors**: Uses CSS custom properties (`var(--accent)`)
- **Spacing**: Consistent spacing (p-6, space-y-6, gap-3)
- **Typography**: Clear hierarchy (text-sm, text-xs)

### Component Patterns

#### Section Headers
```typescript
<div>
  <h3 className="text-sm font-medium text-foreground mb-1">
    Section Name
  </h3>
  <p className="text-xs text-muted-foreground">
    Section description
  </p>
</div>
```

#### Setting Rows
```typescript
<div className="flex items-center justify-between">
  <div className="flex items-center gap-3">
    <div className="p-2 rounded-lg bg-accent/10">
      <Icon className="h-4 w-4 text-accent" />
    </div>
    <div>
      <Label className="text-sm font-medium cursor-pointer">
        Setting Name
      </Label>
      <p className="text-xs text-muted-foreground">
        Setting description
      </p>
    </div>
  </div>
  <Switch className="data-[state=checked]:bg-accent" />
</div>
```

## Dependencies

### Required Imports
```typescript
import { useTheme } from "next-themes";          // Theme management
import { motion, AnimatePresence } from "framer-motion"; // Animations
import { createPortal } from "react-dom";        // Portal rendering
import { Switch } from "@/components/ui/switch"; // Toggle component
import { Label } from "@/components/ui/label";   // Accessible labels
import { useIsMobile } from "@/hooks/use-mobile"; // Responsive detection
```

### Icon System
Uses Lucide React icons consistently:
- **4x4px** size for setting icons
- **5x5px** size for theme check marks
- **4x4px** size for close button

## Accessibility

### Features
- **Keyboard Navigation**: All interactive elements focusable
- **Screen Readers**: Proper labels with `htmlFor` attributes
- **Color Contrast**: Theme previews use sufficient contrast
- **Focus Management**: Natural tab order through settings

### Implementation
```typescript
<Label htmlFor="setting-id" className="text-sm font-medium cursor-pointer">
  Setting Name
</Label>
// ... 
<Switch id="setting-id" />
```

## Performance Considerations

### Rendering Strategy
- **Portal Rendering**: Prevents layout thrashing in parent components
- **Conditional Mounting**: Only renders when `isOpen` is true
- **Hydration Safety**: Waits for `mounted` state before rendering

### Optimization Opportunities
- **Memoization**: Theme options array could be memoized
- **Lazy Loading**: Settings could be loaded on first open
- **Virtual Scrolling**: For many settings (not currently needed)

## Troubleshooting

### Common Issues

#### Mobile Scrolling Not Working
- **Ensure**: `overflow-y-scroll` (not `overflow-y-auto`)
- **Check**: `WebkitOverflowScrolling: 'touch'` is applied
- **Verify**: Container has explicit height, not max-height
- **Confirm**: No conflicting touch-action CSS

#### Z-Index Issues
- **Solution**: Component uses portal rendering
- **Verify**: Z-index values (backdrop: 9998, modal: 9999)

#### Animation Performance
- **Check**: GPU acceleration is enabled
- **Ensure**: Spring animations use appropriate damping/stiffness

## Customization Guide

### Drawer Dimensions
```typescript
// Adjust in className
isMobile 
  ? "bottom-0 left-0 right-0 rounded-t-2xl h-[85vh]"  // Mobile height
  : "top-0 right-0 h-screen w-[420px] max-w-[90vw]"   // Desktop width
```

### Theme Colors
Update the `themeOptions` array with new gradients and colors:
```typescript
{
  label: "Custom",
  icon: CustomIcon,
  value: 'custom',
  gradient: 'linear-gradient(135deg, #custom1, #custom2)',
  badgeColor: '#textColor',
  badgeBg: '#bgColor',
  border: '#borderColor'
}
```

## Future Enhancements

### Planned Features
- **Settings Persistence**: localStorage/API integration
- **Keyboard Shortcuts**: Hotkey configuration
- **Import/Export**: Settings backup/restore
- **Search**: Setting search functionality
- **Categories**: Collapsible setting groups

### Extensibility Points
- **Custom Setting Types**: Beyond boolean toggles
- **Conditional Settings**: Settings that depend on others
- **Setting Validation**: Input constraints and validation
- **Real-time Sync**: Multi-device settings sync
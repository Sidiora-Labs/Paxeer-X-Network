# step.tsx

## Overview

`StepItem` renders a single step in the wallet connector flow: title, description, and a leading indicator (dot when inactive, spinner when loading, check icon when completed). Optional vertical divider to the next step.

## Exports

### `StepItem`

#### Props

| Prop | Type | Required | Default | Description |
|------|------|----------|--------|-------------|
| `active` | `boolean` | No | — | Current step is active |
| `isLoading` | `boolean` | No | — | Step is in loading state (shows spinner) |
| `isCompleted` | `boolean` | No | — | Step is done (shows check icon) |
| `title` | `string` | Yes | — | Step title |
| `description` | `string` | Yes | — | Step description |
| `showDivider` | `boolean` | No | — | Show vertical dashed divider to next step |

## Usage example

```tsx
import { StepItem } from "./step";

<StepItem
  title="Create account"
  description="Sign to create your paxeer-network account"
  active={activeStep === 0}
  isLoading={loading && activeStep === 0}
  isCompleted={activeStep > 0}
  showDivider
/>
```

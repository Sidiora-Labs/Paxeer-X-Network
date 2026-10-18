# displayControl common

## Overview

Shared types for display control components: `IProps` (state + setter) and `DisplayControl` (label + id matching `DisplayControlSettingInterface` keys).

## Exports

### IProps

| Property | Type | Description |
|----------|------|-------------|
| `displayControlState` | `DisplayControlSettingInterface` | Current display toggles |
| `changeDisplayControlState` | `(state: DisplayControlSettingInterface) => void` | Set new state |

### DisplayControl

| Property | Type | Description |
|----------|------|-------------|
| `label` | `string` | Display label (i18n) |
| `id` | `keyof DisplayControlSettingInterface` | Key in DisplayControlSettingInterface |

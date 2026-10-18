# footer.widget

> Location: `packages/ui-scaffold/src/components/footer/footer.widget.tsx`

## Overview

Combines `useFooterScript()` (e.g. WS status) with `Footer` UI. Exports `FooterWidget` and `FooterProps`.

## Props (`FooterProps`)

| Property    | Type                   | Required | Description                                                                                                             |
| ----------- | ---------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------- |
| telegramUrl | `string`               | No       | Telegram link                                                                                                           |
| twitterUrl  | `string`               | No       | Twitter link                                                                                                            |
| discordUrl  | `string`               | No       | Discord link                                                                                                            |
| trailing    | `ReactNode`            | No       | Trailing content                                                                                                        |
| poweredBy   | `boolean \| ReactNode` | No       | Powered-by content. Hidden by default. Use `true` to show the default `Powered by paxeer-network`, or pass a custom ReactNode. |

## Usage example

```tsx
import { FooterWidget } from "@paxeer-network/ui-scaffold";

<FooterWidget twitterUrl="..." poweredBy />
<FooterWidget poweredBy={<span>Powered by My DEX</span>} />
```

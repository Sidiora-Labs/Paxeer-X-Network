# message

## Overview

Optional custom message for the poster: a checkbox to enable the message and an input (max 25 characters). Shows clear button when focused; toast on max length. Uses i18n for label and placeholder.

## Component

### Message

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| message | string | Yes | Current message value. |
| setMessage | (value: string) => void | Yes | Updates message. |
| check | boolean | Yes | Whether message is enabled (checkbox). |
| setCheck | (value: boolean) => void | Yes | Updates checkbox. |

When input has content, `setCheck(true)` is called. Input uses `CloseCircleFillIcon` as suffix when focused to clear.

## Usage example

```tsx
<Message
  message={message}
  setMessage={setMessage}
  check={check}
  setCheck={setCheck}
/>
```

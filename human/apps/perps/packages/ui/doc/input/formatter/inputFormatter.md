# inputFormatter.ts

## inputFormatter.ts 的职责

Defines the `InputFormatter` interface and `InputFormatterOptions` type used by the input formatter system. Formatters implement `onRenderBefore` (format value for display) and `onSendBefore` (format value before submit/send). Used by inputFormatter namespace (number, currency, identifier, etc.) and Input component when a formatter is supplied.

## inputFormatter.ts 对外导出内容

| Name                  | Type | Role                                | Description                                 |
| --------------------- | ---- | ----------------------------------- | ------------------------------------------- |
| InputFormatter        | type | Formatter contract                  | Object with onRenderBefore and onSendBefore |
| InputFormatterOptions | type | Options passed to formatter methods | isFocused, originValue                      |

## InputFormatter 的输入与输出

- **Input**: InputFormatter is an interface; implementations receive (value, options) in onRenderBefore and onSendBefore.
- **Output**: onRenderBefore returns string (display value); onSendBefore returns string (value to send).

## InputFormatter 类型说明

| Field          | Type                                                                | Description                                       |
| -------------- | ------------------------------------------------------------------- | ------------------------------------------------- |
| onRenderBefore | (value: string \| number, options: InputFormatterOptions) => string | Format for display (e.g. add thousands separator) |
| onSendBefore   | (value: string, options: InputFormatterOptions) => string           | Format before submit (e.g. strip non-numeric)     |

## InputFormatterOptions 字段说明

| Field       | Type    | Description              |
| ----------- | ------- | ------------------------ |
| isFocused   | boolean | Whether input is focused |
| originValue | string  | Optional original value  |

## InputFormatter 依赖与调用关系

- **Upstream**: None (pure types).
- **Downstream**: input/formatter (number, currency, identifier), Input component when using formatter prop.

## InputFormatter Example

```ts
import type {
  InputFormatter,
  InputFormatterOptions,
} from "@paxeer-network/ui";

const myFormatter: InputFormatter = {
  onRenderBefore(value, options) {
    return typeof value === "number" ? value.toLocaleString() : value;
  },
  onSendBefore(value) {
    return value.replace(/,/g, "");
  },
};
```

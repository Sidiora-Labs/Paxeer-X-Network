import { InputFormatter, InputFormatterOptions } from "./inputFormatter";

type RegExpFunction = (value: string | number) => string;

type RegexInputFormatter = (regex: RegExp | RegExpFunction) => InputFormatter;

export const createRegexInputFormatter: RegexInputFormatter = (
  regex: RegExp | RegExpFunction,
  onSendBefore?: (value: string, options: InputFormatterOptions) => string,
) => ({
  onRenderBefore: (
    value: string | number,
    options: InputFormatterOptions,
  ): string => {
    if (typeof regex === "function") {
      return regex(String(value));
    }
    const formattedValue = `${value}`.replace(regex, "");
    return formattedValue;
  },
  onSendBefore: (value: string, options: InputFormatterOptions): string => {
    return onSendBefore?.(value, options) || value;
  },
});

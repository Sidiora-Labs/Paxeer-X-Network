import { commify } from "@paxeer-network/utils";
import { InputFormatter, InputFormatterOptions } from "./inputFormatter";

export const decimalPointFormatter: InputFormatter = {
  onRenderBefore: function (
    value: string | number,
    options: InputFormatterOptions
  ): string {
    return String(value);
  },
  onSendBefore: function (
    value: string,
    options: InputFormatterOptions
  ): string {
    if (value === null || value === undefined) return "";
    return replaceComma(value, options?.originValue);
  },
};

const replaceComma = (value: string, originValue?: string) => {
  if (value === null || value === undefined) return "";
  if (value.endsWith(",")) {
    return value.slice(0, -1) + ".";
  }
  if (value.startsWith(",")) {
    return value.slice(0, 1) + ".";
  }

  const originFormatValue = commify(originValue as string)
  if (
    originValue &&
    value.length > originFormatValue.length &&
    value.length - originFormatValue.length === 1
  ) {
    return findAndReplace(value, originFormatValue);
  }
  return value;
};

const findAndReplace = (str1: string, str2: string) => {
  if (str1.length <= str2.length) {
    return str1;
  }

  for (let i = 0; i < str2.length; i++) {
    if (str1[i] !== str2[i]) {
      const differentChar = str1[i];
      const newChar = differentChar === "," ? "." : differentChar;
      return str1.slice(0, i) + newChar + str1.slice(i + 1);
    }
  }
  return str1;
};

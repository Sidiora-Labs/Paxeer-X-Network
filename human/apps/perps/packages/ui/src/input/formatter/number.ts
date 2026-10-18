import { InputFormatter, InputFormatterOptions } from "./inputFormatter";

export const numberFormatter: InputFormatter = {
  onRenderBefore: function (
    value: string | number,
    options: InputFormatterOptions
  ): string {
    return "" + value;
  },
  onSendBefore: function (
    value: string,
    options: InputFormatterOptions
  ): string {
    if (value.startsWith(".")) return `0${value}`;
    if (value === '00') return '0';

    // if (/^0{2,}$/.test(value)) {
    //   return "0";
    // }

    value = value
      .replace(/[^\d.]/g, "")
      .replace(".", "$#$")
      .replace(/\./g, "")
      .replace("$#$", ".");

    return value;
  },
};

import { InputFormatterOptions } from "./inputFormatter";
import { Decimal } from "@paxeer-network/utils";

export const identifierFormatter = (
//  props: {
//   integer?: string;
//   negative?: string;
//  }
) => {
  // const {
  //   integer = '+',
  //   negative = '-',
  // } = props;

  const integer = '+';
  const negative = '-';
  return {
    onRenderBefore: (value: string | number, options: InputFormatterOptions) => {
      if (typeof value === "number") value = value.toString();
      if (!value || value.endsWith(".")) return value;
      const _value = Number(value);
      if (_value) {
        // greater than 0 && not start with +
        if (_value > 0 && !value.startsWith(integer)) return integer + value;
        // greater less 0 && not start with -
        if (_value < 0 && !value.startsWith(negative)) return negative + value;
      }
      return value;
    },
    onSendBefore: (value: string | number, options: InputFormatterOptions) => {
      if (typeof value === "number") value = value.toString();
      if (!value || value.endsWith(".")) return value;
      const _value = Number(value);
      if (_value) {
        // greater than 0
        if (_value > 0) return value.replace(integer, "");
      }
      return value;
    },
  };
};


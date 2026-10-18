import { InputFormatterOptions } from "./inputFormatter";

export const rangeFormatter = (props: {
  max: number;
  min: number;
  dp?: number;
}) => {
  const onBefore = (value: string | number, options: InputFormatterOptions) => {
    if (typeof value === "number") value = value.toString();

    // Handle empty value or value ending with decimal point
    if (value === ".") return "0.";
    if (!value || value.endsWith(".")) return value;

    const { max, min, dp } = props;

    const numValue = Number(value);

    // Handle invalid number
    if (isNaN(numValue)) {
      return "";
    }

    if (numValue < min) {
      return min.toString();
    }

    if (numValue > max) {
      return max.toString();
    }

    // Handle decimal places if dp is specified
    if (dp !== undefined && value.includes(".")) {
      const [integer, decimal] = value.split(".");
      // if decimal length is more than dp, return value without the last digit
      return decimal?.length <= dp ? value : value.slice(0, -1);
    }

    return value;
  };

  return {
    onRenderBefore: onBefore,
    onSendBefore: onBefore,
  };
};

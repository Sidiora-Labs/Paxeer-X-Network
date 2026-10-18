import { ReactElement } from "react";
import { Slot } from "@radix-ui/react-slot";
import { ComponentPropsWithout } from "../helpers/component-props";

type CaseProps<Props = {}> =
  | { [key: string | number]: ReactElement<Props> }
  | ((value: any) => ReactElement<Props>);
type ValueTypes = string | number | (() => any);

type Props<Props = {}> = {
  value: ValueTypes;
  case: CaseProps<Props>;
  default: ReactElement;
} & Props &
  ComponentPropsWithout<"div", "value" | "case" | "default">;

/** @deprecated TODO: remove this component, because vite will throw Maximum call stack size exceeded
 */
export const Match = <T,>(props: Props<T>) => {
  const { value, case: Case, default: Default, ...rest } = props;

  const _value = typeof value === "function" ? value() : value;

  if (typeof Case === "function") {
    const Comp = Case(_value);

    if (Comp) {
      return <Slot children={Comp} {...rest} />;
    }

    if (typeof Default === "undefined") {
      // console.warn('')
      return;
    }

    return <Slot children={Default} {...rest} />;
  }

  const keys = Object.keys(Case);

  while (keys.length) {
    const key = keys.pop();
    if (key === _value.toString()) {
      return <Slot children={Case[key!]} {...rest} />;
    }
  }
  return <Slot children={Default} {...rest} />;
};

import React, { FC, ReactElement } from "react";
import { cn } from "..";
import { Box } from "../box";
import { Flex } from "../flex";
import { Select, SelectProps } from "./select";
import { SelectGroup, SelectItem } from "./selectPrimitive";

export type SelectOption = {
  label: string;
  value: string;
};

export type SelectWithOptionsProps<T = string> = SelectProps<T> & {
  // options: SelectOption[] | (() => Promise<SelectOption[]>);
  currentValue?: string;
  options: SelectOption[];
  optionRenderer?: (
    option: SelectOption,
    // & {
    //   size?: SizeType;
    //   index: number;
    // }
  ) => ReactElement;
  // loading?: boolean;
  testid?: string;
  prefix?: string;
};

export const defaultOptionRenderer = (
  option: SelectOption,
  currentValue?: string,
) => {
  return (
    <SelectItem
      key={option.value}
      value={option.value}
      className={cn("oui-relative oui-cursor-pointer")}
      data-testid={`oui-testid-selectItem-${option.value
        ?.toLowerCase()
        ?.replace(" ", "_")}`}
    >
      {option.label}
      {currentValue == option.value && (
        <Box
          width={4}
          height={4}
          gradient="brand"
          r="full"
          className="oui-absolute oui-end-2 oui-top-1/2 -oui-translate-y-1/2"
        />
      )}
    </SelectItem>
  );
};

export const SelectWithOptions: FC<SelectWithOptionsProps> = (props) => {
  const {
    children,
    options,
    optionRenderer = defaultOptionRenderer,
    currentValue,
    ...rest
  } = props;

  // const [] = useState<SelectOption[]>([]);

  return (
    <Select {...rest}>
      <SelectGroup>
        {options.map((option, index) => {
          // return optionRenderer({
          //   ...option,
          //   // value: option,
          //   size: props.size,
          //   index,
          // });
          return React.cloneElement(optionRenderer(option, currentValue), {
            size: props.size,
            key: index,
            index,
          });
        })}
      </SelectGroup>
    </Select>
  );
};

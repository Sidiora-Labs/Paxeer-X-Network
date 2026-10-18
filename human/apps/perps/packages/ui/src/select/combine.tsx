import React, { KeyboardEvent, useRef, useState } from "react";
import * as SelectPrimitive from "@radix-ui/react-select";
import { CaretDownIcon } from "../icon";
import { Input } from "../input";
import { PopoverRoot, PopoverAnchor, PopoverContent } from "../popover";
// import { SizeType } from "../helpers/sizeType";
import { ScrollArea } from "../scrollarea";
import { type SelectVariantProps } from "./select";
import { selectVariants } from "./selectPrimitive";
import { SelectOption, SelectWithOptionsProps } from "./withOptions";

export type CombineSelectProps = {
  placeholder?: string;
} & SelectWithOptionsProps;

export const CombineSelect: React.FC<CombineSelectProps> = (props) => {
  const { options, variant, valueFormatter, ...rest } = props;
  const [keyword, setKeyword] = useState<string>("");
  // const [open, setOpen] = useState<boolean>(props.defaultOpen || false);
  const [value, setValue] = useState<string | undefined>(props.value ?? "");
  const { trigger } = selectVariants({
    size: props.size,
    variant,
  });

  const [selectedIndex, setSelectedIndex] = useState(() =>
    typeof props.value === "undefined"
      ? -1
      : options.findIndex((option) => option.value === props.value),
  );

  const [focused, setFocused] = useState<boolean>(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const onFocus = () => {
    setFocused(true);
    // setOpen(true);
  };

  const filteredOptions = !keyword
    ? options
    : options.filter((option) => {
        if (option.value.toLowerCase().includes(keyword.toLowerCase()))
          return true;
      });

  // const onKeyDown = useCallback(
  //   (e: React.KeyboardEvent) => {
  //     console.log(e.code);
  //   },
  //   [filteredOptions]
  // );

  return (
    <PopoverRoot open={focused}>
      <PopoverAnchor>
        <Input
          ref={inputRef}
          size={props.size}
          autoComplete={"off"}
          onFocus={onFocus}
          onBlur={() => {
            setFocused(false);
          }}
          placeholder={props.placeholder ?? "All"}
          value={
            focused
              ? keyword
              : typeof valueFormatter === "function"
                ? (valueFormatter(value ?? "", {
                    placeholder: props.placeholder,
                  }) as string)
                : value
          }
          onValueChange={(value) => {
            setKeyword(value);
          }}
          // onKeyDown={onKeyDown}

          classNames={{
            root: trigger({
              className: "oui-w-24 oui-peer",
            }),
            input: "oui-text-base-contrast-54 oui-font-semibold",
          }}
          data-state={focused ? "open" : "closed"}
          suffix={
            <SelectPrimitive.Icon
              onMouseDown={(e) => {
                e.preventDefault();
                focused ? inputRef.current?.blur() : inputRef.current?.focus();
              }}
              asChild
              className="oui-transition-transform peer-data-[state=open]:oui-rotate-180 peer-data-[state=closed]:oui-rotate-0 oui-mx-2"
            >
              <CaretDownIcon
                size={12}
                className="oui-text-inherit"
                // opacity={1}
              />
            </SelectPrimitive.Icon>
          }
        />
      </PopoverAnchor>
      <PopoverContent
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          setKeyword("");
        }}
        className={"oui-w-[var(_--radix-popover-trigger-width)] oui-p-1"}
      >
        <ScrollArea className="oui-h-[200px]">
          {filteredOptions.map((option, index) => {
            return (
              <SelectItem
                option={option}
                key={index}
                size={props.size}
                activated={selectedIndex === index}
                onClick={(value) => {
                  setValue(value.value);
                  setSelectedIndex(index);
                  props.onValueChange?.(value.value);
                  inputRef.current?.blur();
                  // setKeyword(value.value);
                }}
              />
            );
          })}
        </ScrollArea>
      </PopoverContent>
    </PopoverRoot>
  );
};

const SelectItem = (props: {
  option: SelectOption;
  size?: SelectVariantProps["size"];
  activated: boolean;
  onClick: (item: SelectOption) => void;
}) => {
  const { item } = selectVariants({
    size: props.size,
  });
  return (
    <button
      className={item({
        className: `oui-text-base-contrast-54 oui-w-full ${
          props.activated ? "oui-bg-base-7" : ""
        }`,
      })}
      onMouseDown={(e) => {
        e.preventDefault();
        props.onClick(props.option);
      }}
    >
      {props.option.label}
    </button>
  );
};

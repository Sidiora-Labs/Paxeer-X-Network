import { PropsWithChildren, type ReactNode, useMemo } from "react";
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
} from "./dropdown";
import type { DropdownMenuContentProps } from "@radix-ui/react-dropdown-menu";
import { SizeType } from "../helpers/sizeType";
import { Flex } from "../flex";
import { Box } from "../box";

export type MenuItem = {
  label: string;
  value: string;
  testId?: string;
};

type DropdownMenuProps = {
  currentValue?: string;
  menu: MenuItem[];
  onSelect?: (item: MenuItem) => void;
  render?: (item: MenuItem, index: number) => ReactNode;
  size?: SizeType;
} & DropdownMenuContentProps;

const SimpleDropdownMenu = (props: PropsWithChildren<DropdownMenuProps>) => {
  const { currentValue, menu, render, size, children, ...contentProps } = props;

  const items = useMemo(() => {
    if (typeof props.render === "function") {
      return props.menu.map((item, index) => {
        return props.render?.(item, index);
      });
    }

    return props.menu.map((item) => (
      <DropdownMenuItem
        textValue={item.value}
        key={item.value}
        onSelect={(event) => {
          props.onSelect?.(item);
        }}
        size={size}
        data-testid={item.testId}
      >
        <Flex justify={"between"} width={"100%"}>
          {item.label}
          {currentValue == item.value && <Box width={4} height={4} gradient="primary" r="full"/>}
        </Flex>
      </DropdownMenuItem>
    ));
  }, [props.menu, props.render, currentValue]);
  return (
    <DropdownMenuRoot>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent
          onCloseAutoFocus={(event) => {
            event.preventDefault();
          }}
          size={size}
          {...contentProps}
        >
          {items}
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenuRoot>
  );
};

export { SimpleDropdownMenu };

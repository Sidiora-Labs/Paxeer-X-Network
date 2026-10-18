import {
  forwardRef,
  SelectHTMLAttributes,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { VariantProps } from "tailwind-variants";
import { ChevronDownIcon, ChevronUpIcon, cn } from "..";
import { SelectOption } from "../select/withOptions";
import { ActionSheet, ActionSheetItem } from "../sheet";
import { tv } from "../utils/tv";

const pickerVariants = tv({
  base: "oui-flex oui-flex-row oui-items-stretch oui-rounded-md oui-cursor-pointer oui-border oui-border-line-6 oui-bg-line-4 oui-text-base-contrast-54",
  variants: {
    size: {
      sm: "oui-px-2 oui-h-[22px] oui-text-2xs",
      md: "oui-px-2 oui-h-[24px] oui-text-2xs",
      base: "oui-px-3 oui-h-[40px]",
      lg: "oui-px-6 oui-py-3",
    },
    fullWidth: {
      true: "oui-w-full",
    },
    disabled: {
      true: "oui-opacity-50 oui-cursor-not-allowed",
    },
    color: {
      // primary: "text-primary-darken",
      base: "oui-text-base-contract",
      buy: "oui-text-trade-profit",
      sell: "oui-text-trade-loss",
    },
  },
  defaultVariants: {
    size: "base",
    color: "base",
  },
});

export interface PickerProps
  extends
    Omit<
      SelectHTMLAttributes<HTMLSelectElement>,
      "disabled" | "size" | "color" | "value"
    >,
    VariantProps<typeof pickerVariants> {
  loading?: boolean;
  label?: string;
  placeholder?: string;
  options: SelectOption[];
  value?: SelectOption | string | number;
  onValueChange?: (value: any) => void;
  valueRenderer?: (
    value: any,
    options: { open?: boolean; data?: any },
  ) => React.ReactNode;
}

export type PickerRef = {};

export const Picker = forwardRef<PickerRef, PickerProps>(
  (originalProps, ref) => {
    const {
      size,
      color,
      value,
      label,
      placeholder,
      className,
      options,
      fullWidth,
      disabled,
      valueRenderer,
      ...props
    } = originalProps;

    const [open, setOpen] = useState(false);

    const selectedItem = useMemo<SelectOption | undefined>(() => {
      if (value && !!(value as SelectOption).value) {
        return value as SelectOption;
      }

      if (typeof value === "number" || typeof value === "string") {
        const option = options.find((option) => option.value === value);
        if (option) {
          return option;
        }
      }
    }, [value, options]);

    const text = useMemo(() => {
      if (selectedItem) {
        return selectedItem.label;
      }
      return placeholder || label || "";
    }, [selectedItem, label, placeholder]);

    const actions = useMemo<ActionSheetItem[]>(() => {
      return [...options, "---", "Cancel"];
    }, [options]);

    // const triggerRef = useRef(null);
    // const [dropdownWidth, setDropdownWidth] = useState(218);

    // useEffect(() => {
    //   if (triggerRef.current) {
    //     // @ts-ignore
    //     const width = triggerRef.current.offsetWidth;
    //     setDropdownWidth(width);
    //   }
    // }, []);

    return (
      <ActionSheet
        actionSheets={actions}
        onOpenChange={setOpen}
        open={open}
        onClose={() => setOpen(false)}
        // @ts-ignore
        value={selectedItem}
        onValueChange={props.onValueChange}
      >
        {typeof valueRenderer === "function" ? (
          valueRenderer(selectedItem?.value || props.defaultValue, {
            open: open,
            data: (selectedItem as any)?.data,
          })
        ) : (
          <div
            className={cn(
              "oui-gap-x-1",
              "oui-text-base-contrast-54",
              "oui-rounded-md",
              "oui-bg-base-8",
              pickerVariants({
                size,
                color,
                fullWidth,
                disabled: disabled || options.length === 0,
                className,
              }),
            )}
          >
            <div className="oui-flex oui-flex-1 oui-items-center oui-justify-start oui-text-2xs oui-text-inherit">
              {text}
            </div>
            <div className="oui-flex oui-items-center">
              {open ? (
                <ChevronUpIcon size={14} color="white" />
              ) : (
                <ChevronDownIcon size={14} color="white" />
              )}
            </div>
          </div>
        )}
      </ActionSheet>
    );
  },
);

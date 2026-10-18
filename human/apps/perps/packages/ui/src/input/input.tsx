import React, { type InputHTMLAttributes, useId } from "react";
import { cnBase, cn, type VariantProps } from "tailwind-variants";
import { tv } from "../utils/tv";
import { BaseInput, BaseInputProps } from "./baseInput";
import { InputPrefix } from "./prefix";
import { InputSuffix } from "./suffix";

const inputVariants = tv(
  {
    slots: {
      input: [
        "oui-w-full oui-bg-transparent",
        "oui-bg-transparent",
        "oui-flex-1",
        "focus-visible:oui-outline-none",
        "oui-flex",
        "placeholder:oui-text-base-contrast-20",
        "placeholder:oui-text-xs",
        "oui-tabular-nums",
        "oui-text-base-contrast",
        "autofill:oui-bg-transparent",
        "oui-input-input",
        "disabled:oui-cursor-not-allowed",
        "oui-peer",
      ],
      box: [
        "oui-rounded",
        "oui-bg-base-6",
        "oui-flex",
        "oui-items-center",
        "oui-outline",
        "oui-outline-offset-0",
        "oui-outline-1",
        "oui-outline-transparent",
        "focus-within:oui-outline-primary-light",
        "oui-input-root",
      ],
      additional: [
        "oui-h-full oui-flex oui-flex-col oui-justify-center oui-px-2 oui-text-base-contrast",
      ],
      closeButton: [
        "oui-cursor-pointer",
        "oui-invisible",
        "peer-focus:oui-visible",
      ],
    },
    variants: {
      // variant: {
      //   outline:{
      //     box:
      //   }
      // },
      size: {
        xs: {
          input: ["oui-h-6", "oui-text-2xs", "placeholder:oui-text-2xs"],
          box: ["oui-h-6"],
          additional: ["oui-text-2xs"],
        },
        sm: {
          input: ["oui-h-7", "oui-text-2xs", "placeholder:oui-text-2xs"],
          box: ["oui-h-7"],
          additional: ["oui-text-2xs"],
        },
        md: {
          input: ["oui-h-8", "oui-text-2xs", "placeholder:oui-text-2xs"],
          box: ["oui-h-8"],
          additional: ["oui-text-2xs"],
        },
        lg: {
          input: ["oui-h-10", "oui-text-sm", "placeholder:oui-text-sm"],
          box: ["oui-h-10 oui-rounded-md"],
          additional: ["oui-text-sm"],
        },
        xl: {
          input: ["oui-h-12", "oui-text-base", "placeholder:oui-text-base"],
          box: ["oui-h-12 oui-rounded-md"],
          additional: ["oui-text-sm"],
        },
      },
      color: {
        success: {
          box: ["oui-outline-success", "focus-within:oui-outline-success"],
          input: ["oui-text-success"],
        },
        danger: {
          box: ["oui-outline-danger", "focus-within:oui-outline-danger"],
          input: ["oui-text-danger"],
        },
        warning: {
          box: [
            "oui-outline-warning-darken",
            "focus-within:oui-outline-warning-darken",
          ],
          input: ["oui-text-warning-darken"],
        },
        default: {
          box: ["oui-outline-transparent"],
        },
      },
      disabled: {
        true: {
          input: ["oui-cursor-not-allowed", "oui-text-base-contrast-20"],
          box: ["oui-bg-base-5"],
        },
      },
      // RTL-EXCLUDE: Keep `pl` / `pr` variants mapped to physical padding.
      // These variant names are part of the Input API and callers use them to
      // reserve explicit left/right space for prefix/suffix layouts.
      pl: {
        true: {
          box: "oui-pl-3",
        },
        false: {
          box: "oui-pl-0",
        },
      },
      pr: {
        true: {
          box: "oui-pr-3",
        },
        false: {
          box: "oui-pr-0",
        },
      },
      fullWidth: {
        true: {
          box: "oui-w-full",
        },
      },
      align: {
        center: {
          input: "oui-text-center",
        },
        left: {
          input: "oui-text-start",
        },
        right: {
          input: "oui-text-end",
        },
      },
    },
    //   compoundVariants: [{ size: "default", className: ["oui-bg-transparent"] }],

    defaultVariants: {
      size: "lg",
    },
  },
  {
    responsiveVariants: ["md", "lg"],
  },
);

// @ts-ignore
interface InputProps<T = string>
  extends BaseInputProps<T>, VariantProps<typeof inputVariants> {
  prefix?: string | React.ReactNode;
  suffix?: string | React.ReactNode;
  fullWidth?: boolean;
  onClear?: () => void;
  classNames?: {
    input?: string;
    root?: string;
    additional?: string;
    clearButton?: string;
    prefix?: string;
    suffix?: string;
  };
}

const Input = React.forwardRef<HTMLInputElement, InputProps>((props, ref) => {
  const {
    size,
    disabled,
    color,
    suffix,
    prefix,
    id,
    pl,
    pr,
    fullWidth,
    className,
    classNames,
    onClear,
    align,
    ...inputProps
  } = props;

  const { input, box, additional, closeButton } = inputVariants({
    size,
    disabled,
    color,
    fullWidth,
    align,
    // className: cnBase(className, classes?.root),

    pl: typeof prefix === "undefined" || pl,
    pr: typeof suffix === "undefined" || pr,
  });
  const cid = useId();

  const suffixElement =
    typeof onClear !== "undefined" ? (
      <ClearButton
        className={closeButton({ className: classNames?.clearButton })}
        onClick={() => {
          onClear?.();
        }}
        value={inputProps.value as string | number}
      />
    ) : (
      suffix
    );

  return (
    <div className={box({ className: cnBase(className, classNames?.root) })}>
      <InputPrefix
        id={id || cid}
        prefix={prefix}
        className={additional({
          className: cnBase(
            "oui-input-prefix",
            classNames?.additional,
            classNames?.prefix,
          ),
        })}
      />
      <BaseInput
        {...inputProps}
        id={id || cid}
        disabled={disabled}
        ref={ref}
        className={input({ align, className: classNames?.input })}
      />
      <InputSuffix
        id={id || cid}
        suffix={suffixElement}
        className={additional({
          className: cnBase(
            "oui-input-suffix",
            classNames?.additional,
            classNames?.suffix,
          ),
        })}
      />
    </div>
  );
});

const ClearButton = React.forwardRef<
  HTMLButtonElement,
  { onClick: () => void; value: string | number; className?: string }
>((props, ref) => {
  return (
    <button
      onMouseDown={(event) => {
        event.preventDefault();
        props.onClick();
      }}
      ref={ref}
      className={cnBase("oui-input-clear-btn", props.className)}
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
        className="oui-fill-base-contrast-20"
      >
        <path d="M8 1.302a6.667 6.667 0 1 0 0 13.333A6.667 6.667 0 0 0 8 1.302m-2 4c.17 0 .349.057.479.187l1.52 1.521L9.52 5.49a.68.68 0 0 1 .48-.188c.17 0 .348.057.479.187.26.261.26.698 0 .96l-1.52 1.52 1.52 1.52c.26.261.26.698 0 .96a.687.687 0 0 1-.959 0L8 8.926l-1.521 1.521a.686.686 0 0 1-.959 0 .686.686 0 0 1 0-.959l1.521-1.52-1.52-1.52a.686.686 0 0 1 0-.96A.68.68 0 0 1 6 5.302" />
      </svg>
    </button>
  );
});

ClearButton.displayName = "ClearButton";

Input.displayName = "Input";

export { Input, inputVariants };

export type { InputProps };

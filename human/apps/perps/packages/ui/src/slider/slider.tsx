import * as React from "react";
import { Fragment, useMemo } from "react";
import * as SliderPrimitive from "@radix-ui/react-slider";
import { cnBase, VariantProps } from "tailwind-variants";
import { cn } from "..";
import { tv } from "../utils/tv";
import { convertValueToPercentage, getThumbInBoundsOffset } from "./utils";

const sliderVariants = tv({
  slots: {
    root: "oui-relative oui-flex oui-w-full oui-touch-none oui-select-none oui-items-center",
    thumb: [
      "oui-block",
      "oui-h-[10px]",
      "oui-w-[10px]",
      "oui-rounded-full",
      "oui-border-[2px]",
      "oui-border-primary",
      "oui-bg-base-6",
      "oui-shadow",
      "oui-group",
      "oui-transition-colors",
      "focus-visible:oui-outline-none",

      // "focus-visible:oui-ring-1",
      // "focus-visible:oui-ring-ring",
      // "focus:oui-w-4",
      // "focus:oui-h-4",
      "focus:oui-shadow-[0_0_0_8px]",
      "focus:oui-shadow-base-contrast/20",
      "focus:oui-h-[14px]",
      "focus:oui-w-[14px]",
      // "focus:oui-border-[3px]",
      "data-[disabled]:oui-pointer-events-none",
      "data-[disabled]:oui-border-base-2",
      "data-[disabled]:oui-hidden",
    ],
    track:
      "oui-relative oui-h-[8px] oui-w-full oui-grow oui-overflow-hidden oui-rounded-full",

    trackInner:
      "oui-absolute oui-start-0 oui-end-0 oui-h-[2px] oui-top-[3px]  oui-pointer-events-none oui-bg-base-2",
    range:
      "oui-absolute oui-h-[2px] oui-top-[3px] oui-bg-primary data-[disabled]:oui-bg-base-2",
    mark: "oui-absolute oui-top-[1px] oui-w-[6px] oui-h-[6px] oui-rounded oui-border oui-border-base-2 oui-bg-base-6 oui-pointer-events-none oui-translate-x-[-50%]",
    tips: [
      "oui-absolute",
      "oui-hidden",
      "oui-rounded",
      "oui-drop-shadow",
      "oui-w-[36px]",
      "oui-h-[19px]",
      "oui-translate-x-[-12px]",
      "oui-top-[-28px]",
      "oui-font-semibold",
      "oui-text-center",
      "group-focus:oui-inline-block",
      "after:oui-block",
      "after:oui-absolute",
      "after:oui-bottom-[-8px]",
      "after:oui-w-0",
      "after:oui-h-0",
      "after:oui-border-4",
      "after:oui-left-1/2",
      "after:oui-translate-x-[-50%]",
      "after:oui-border-solid",
      "after:oui-border-transparent",
      "after:oui-border-t-inherit",
      "oui-text-base-5",
      "oui-text-2xs",
    ],
  },
  variants: {
    color: {
      primary: {
        thumb: ["oui-border-primary", "oui-bg-base-5"],
        range: "oui-bg-primary",
        tips: "oui-bg-primary after:oui-border-t-primary",
      },
      primaryLight: {
        thumb: ["oui-border-primary-light", "oui-bg-base-5"],
        range: "oui-bg-primary-light",
        tips: "oui-bg-primary-light after:oui-border-t-primary-light",
      },
      buy: {
        thumb: ["oui-border-success", "oui-bg-base-5"],
        range: "oui-bg-success",
        tips: ["oui-bg-success after:oui-border-t-success"],
      },
      sell: {
        thumb: ["oui-border-danger", "oui-bg-base-5"],
        range: "oui-bg-danger",
        tips: ["oui-bg-danger after:oui-border-t-danger"],
      },
    },
  },
});

export type SliderMarks = { value: number; label: string }[];

type SliderProps = React.ComponentPropsWithoutRef<typeof SliderPrimitive.Root> &
  VariantProps<typeof sliderVariants> & {
    // showMarks?: boolean;
    marks?: SliderMarks;
    markCount?: number;
    markLabelVisible?: boolean;
    showTip?: boolean;
    tipFormatter?: (
      value: number,
      min: number,
      max: number,
      percent: number,
    ) => string | React.ReactNode;
    classNames?: {
      root?: string;
      thumb?: string;
      track?: string;
      range?: string;
      trackInner?: string;
    };
  };

const BaseSlider = React.forwardRef<
  React.ElementRef<typeof SliderPrimitive.Root>,
  SliderProps
>((oriProps, ref) => {
  const {
    className,
    color,
    marks,
    markCount,
    classNames,
    markLabelVisible,
    showTip,
    onValueChange,
    value: __propsValue,
    ...props
  } = oriProps;
  const { track, range, thumb, root, trackInner, mark, tips } = sliderVariants({
    color,
  });

  const [innerValue, setInvalue] = React.useState(__propsValue);

  React.useEffect(() => {
    setInvalue((prev) => {
      if (!prev) {
        return __propsValue;
      }
      if (__propsValue?.some((v, i) => v !== prev[i])) {
        return __propsValue;
      }
      return prev;
    });
  }, [__propsValue]);

  const innerMasks = useMemo<SliderMarks>(() => {
    if (Array.isArray(marks) && marks.length > 0) {
      return marks;
    }

    const _max = props.max ?? 100;
    const _min = props.min ?? 0;

    if (typeof markCount !== "undefined") {
      const marks: SliderMarks = [];

      // calculate the range from min to max
      const range = _max - _min;
      const piece = range / markCount;

      for (let i = 0; i <= markCount; i++) {
        const value = _min + i * piece;
        marks.push({
          value,
          label: `${value}`,
        });
      }

      return marks;
    }

    return [];
  }, [marks, markCount, props.max, props.min]);

  const onValueChangeInner = (value: number[]) => {
    setInvalue(value);

    onValueChange?.(value);
  };

  return (
    <SliderPrimitive.Root
      ref={ref}
      className={root({ className })}
      value={onValueChange ? __propsValue : innerValue}
      onValueChange={onValueChange ? onValueChange : onValueChangeInner}
      {...props}
    >
      <SliderPrimitive.Track
        className={track({ className: classNames?.track })}
      >
        <div className={trackInner({ className: classNames?.trackInner })} />
        <SliderPrimitive.Range
          className={range({ className: classNames?.range })}
        />
      </SliderPrimitive.Track>
      {Array.isArray(innerMasks) && innerMasks.length > 0 && (
        <Marks
          value={innerValue}
          color={color}
          marks={innerMasks}
          isInnerMask={!Array.isArray(marks) || marks.length === 0}
          min={props.min}
          max={props.max}
          markLabelVisible={markLabelVisible}
          disabled={props.disabled}
          className={mark()}
          step={props.step}
        />
      )}
      <SliderPrimitive.Thumb
        className={thumb({
          className: cn(classNames?.thumb, "oui-slider-thumb"),
        })}
      >
        {showTip && (
          <SliderTip
            value={innerValue}
            className={tips({ color })}
            max={props.max ?? 100}
            min={0}
            tipFormatter={props.tipFormatter}
          />
        )}
      </SliderPrimitive.Thumb>
    </SliderPrimitive.Root>
  );
});

BaseSlider.displayName = SliderPrimitive.Root.displayName;

export type SliderMarksProps = {
  value?: number[];
  marks?: SliderMarks;
  color?: "primary" | "buy" | "sell" | "primaryLight";
  // width: number;
  min?: number;
  max?: number;
  disabled?: boolean;
  markLabelVisible?: boolean;
  isInnerMask?: boolean;
  className?: string;
  step?: number;
};

const Marks: React.FC<SliderMarksProps> = (props) => {
  const {
    marks,
    value,
    isInnerMask,
    markLabelVisible,
    className,
    color = "primary",
  } = props;
  const _value = useMemo(() => value?.[0] ?? 0, [value]);
  const selIndex = useMemo(() => {
    if (typeof props.step === "undefined") {
      return undefined;
    }
    return Math.floor(_value / props.step);
  }, [_value, props.step]);

  const colorCls = useMemo(() => {
    switch (color) {
      case "primary":
        return "oui-border-primary oui-bg-primary";
      case "buy":
        return "oui-border-trade-profit oui-bg-trade-profit";
      case "sell":
        return "oui-border-trade-loss oui-bg-trade-loss";
      case "primaryLight":
        return "oui-border-primary-light oui-bg-primary-light";
    }
  }, [color]);

  const textCls = useMemo(() => {
    switch (color) {
      case "primary":
        return "oui-text-primary";
      case "buy":
        return "oui-texttrade-profit";
      case "sell":
        return "oui-text-trade-loss";
      case "primaryLight":
        return "oui-text-primary-light";
    }
  }, [color]);

  return (
    <>
      {marks?.map((mark, index) => {
        const percent = convertValueToPercentage(
          mark.value,
          props.min ?? 1,
          props.max ?? marks.length - 1,
        );
        // const percent = convertValueToPercentage(index, 0, marks.length - 1);
        // const percent = ((100 - 2 * 6) / (marks.length - 1)) * index;
        const thumbInBoundsOffset = getThumbInBoundsOffset(6, percent, 1);
        const __value = isInnerMask ? mark.value : index;
        // console.log("_ value", isInnerMask, _value, selIndex, mark, __value, percent);

        const active =
          (isInnerMask ? _value >= __value : _value >= mark.value) &&
          _value >= 0 &&
          !props.disabled;

        const classNames = active ? colorCls : "";

        return (
          <Fragment key={index}>
            {/* Only draw mark dots when label is not empty (for external marks) or always draw (for internal marks) */}
            {(!isInnerMask ? mark.label : true) && (
              <span
                className={cnBase(className, classNames)}
                style={{ left: `calc(${percent}% + ${thumbInBoundsOffset}px)` }}
              />
            )}
            {!props.disabled && markLabelVisible && mark.label && (
              <span
                data-testid={`oui-testid-slider-mark-label-${mark.label}`}
                key={index}
                className={cn(
                  "oui-absolute oui-top-[16px] oui-text-2xs xl:oui-text-xs oui-text-base-contrast-54 oui-cursor-pointer oui-translate-x-[-50%]",
                  selIndex === index && textCls,
                )}
                style={{ left: `calc(${percent}% + ${thumbInBoundsOffset}px)` }}
              >
                {mark.label}
              </span>
            )}
          </Fragment>
        );
      })}
    </>
  );
};

export interface SliderTipProps {
  value?: number[];
  className?: string;
  min: number;
  max: number;
  tipFormatter?: (
    value: number,
    min: number,
    max: number,
    percent: number,
  ) => string | React.ReactNode;
}

export const SliderTip: React.FC<SliderTipProps> = (props) => {
  const { className, min, max } = props;
  const value = props.value?.[0] ?? 0;
  const percent = convertValueToPercentage(value, min, max);
  return (
    <span className={className} style={{ lineHeight: "19px" }}>
      {props.tipFormatter?.(value, min, max, percent) ??
        `${percent.toFixed()}%`}
    </span>
  );
};

const SingleSlider = React.forwardRef<
  React.ElementRef<typeof SliderPrimitive.Root>,
  Omit<SliderProps, "value" | "onValueChange" | "onValueCommit"> & {
    value: number;
    onValueChange?: (value: number) => void;
    onValueCommit?: (value: number) => void;
  }
>((props, ref) => {
  const _value = useMemo(() => [props.value], [props.value]);

  return (
    <BaseSlider
      {...props}
      value={_value}
      ref={ref}
      onValueChange={
        typeof props.onValueChange === "function"
          ? (values: number[]) => {
              props.onValueChange!(values[0]);
            }
          : undefined
      }
      onValueCommit={
        typeof props.onValueCommit === "function"
          ? (values: number[]) => {
              props.onValueCommit!(values[0]);
            }
          : undefined
      }
    />
  );
});

SingleSlider.displayName = "SingleSlider";

type SliderType = typeof BaseSlider & {
  single: typeof SingleSlider;
};

const Slider = BaseSlider as SliderType;

Slider.single = SingleSlider;

export { Slider };

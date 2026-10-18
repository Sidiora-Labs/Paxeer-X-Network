import "./install";

export * from "./button";
export { Box, boxVariants } from "./box";
export { Grid } from "./grid";
export { Flex } from "./flex";
export * from "./typography";
export { Spinner } from "./spinner";
export { Input, inputFormatter, InputAdditional, TextField } from "./input";
export { Checkbox } from "./checkbox";
export { Switch } from "./switch";
export { Badge, type BadgeProps } from "./badge/badge";
export { Logo, type LogoProps } from "./logo/logo";
// export * from "./tag/tag";
export * from "./tooltip";
export * from "./table";
export * from "./scrollarea";
export * from "./dialog";
export * from "./sheet";
export * from "./divider";
export * from "./tabs";
export { PaginationItems } from "./pagination";
export { Select, SelectItem, type SelectOption } from "./select";
export * from "./popover";
export * from "./card";
export * from "./pickers";
export * from "./slider";
export * from "./toast";
export * from "./listView";
export * from "./collapsible";
export * from "./marquee";

export * from "./dropdown";

export * from "./icon";
export * from "./modal";
export { EVMAvatar, Avatar } from "./avatar";

export type { ButtonProps } from "./button";
export type { BoxProps } from "./box";
export type { FlexProps } from "./flex";
export type { TextProps, NumeralProps, FormattedTextProps } from "./typography";
export type {
  InputProps,
  TextFieldProps,
  InputFormatter,
  InputFormatterOptions,
  InputWithTooltipProps,
} from "./input";
export type { SpinnerProps } from "./spinner";
export type { ChainSelectProps, SelectProps } from "./select";
export type { SizeType } from "./helpers/sizeType";

export {
  PaxeerNetworkThemeProvider,
  type PaxeerNetworkThemeProviderProps,
} from "./provider/paxeerNetworkThemeProvider";
export {
  usePaxeerNetworkTheme,
  type ThemeConfig,
  type TradingViewColorConfig,
} from "./provider/paxeerNetworkThemeContext";
export * from "./plugin";

//===== Misc widgets
export { Either } from "./misc/either";
// TODO: remove this component, because vite will throw Maximum call stack size exceeded
// export { Match } from "./misc/switch";

//===== re-exported
export { cnBase as cn } from "tailwind-variants";
export type { VariantProps } from "tailwind-variants";

//===== utils
export * from "./utils";
export { tv } from "./utils/tv";
export { default as toast } from "react-hot-toast";

export * from "embla-carousel-react";
export { default as useEmblaCarousel } from "embla-carousel-react";
export * from "embla-carousel-auto-scroll";
export { default as AutoScroll } from "embla-carousel-auto-scroll";
export * from "embla-carousel-autoplay";
export { default as Autoplay } from "embla-carousel-autoplay";
export * from "embla-carousel";

export * from "./hooks";

export * from "./tips";
export * from "./locale";
export * from "./scrollIndicator";
export * from "./status";

//===== tailwind
export * as OUITailwind from "./tailwind";
export {
  PURPLE_DARK_THEME_CSS_VARS,
  DARK_THEME_CSS_VARS,
  LIGHT_THEME_CSS_VARS,
  getContrastTextRgb,
  type ThemeCssVars,
} from "./tailwind/theme";

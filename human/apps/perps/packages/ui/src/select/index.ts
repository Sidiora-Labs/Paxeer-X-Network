import { CombineSelect } from "./combine";
import { Select as SelectComponent } from "./select";
import { TokenSelect } from "./tokens";
import { SelectWithOptions } from "./withOptions";

export {
  SelectRoot,
  SelectGroup,
  SelectValue,
  SelectTrigger,
  SelectContent,
  SelectLabel,
  SelectItem,
  SelectSeparator,
  SelectScrollUpButton,
  SelectScrollDownButton,
} from "./selectPrimitive";

export type SelectType = typeof SelectComponent & {
  options: typeof SelectWithOptions;
  tokens: typeof TokenSelect;
  combine: typeof CombineSelect;
};

const Select = SelectComponent as SelectType;
Select.options = SelectWithOptions;
Select.combine = CombineSelect;
Select.tokens = TokenSelect;

export { Select };

export type { SelectProps } from "./select";
export type { SelectWithOptionsProps, SelectOption } from "./withOptions";
export type { ChainSelectProps } from "./chains";

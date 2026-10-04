import { KitOptionList } from "../../kit/display";

export function ChoiceField({
  label,
  value,
  options,
  onChange,
}: Readonly<{
  label: string;
  value: string;
  options: readonly Readonly<{ value: string; label: string; disabled?: boolean }>[];
  onChange: (value: string) => void;
}>) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-sm font-semibold text-foreground">{label}</span>
      <KitOptionList
        aria-label={label}
        value={value}
        onValueChange={onChange}
        items={options.map((option) => ({
          value: option.value,
          label: option.label,
          disabled: option.disabled === true,
        }))}
      />
    </div>
  );
}

import { FC, useEffect } from "react";
import { CheckIcon, Input, inputFormatter, Tooltip } from "@paxeer-network/ui";

export const InnerInput: FC<{
  inputRef: any;
  value: string;
  setValue: any;
  setEditing: any;
  error?: string;
  handleKeyDown: (e: any) => void;
  onClick: (e: any) => void;
  onClose: (e: any) => void;
  onFocus?: (e: any) => void;
  onBlur?: (e: any) => void;
  dp: number;
  hintInfo?: string;
}> = (props) => {
  const {
    inputRef,
    dp,
    value,
    setValue,
    setEditing,
    error,
    handleKeyDown,
    onClick,
    onClose,
    onFocus,
    onBlur,
    hintInfo,
  } = props;

  useEffect(() => {
    const input = inputRef.current;
    if (input) {
      const length = input.value.length;
      input.setSelectionRange(length, length);
    }
    setEditing(true);
  }, []);
  const open = (hintInfo?.length || 0) > 0;
  return (
    <Tooltip content={hintInfo} open={open}>
      <Input
        ref={inputRef}
        type="text"
        size="sm"
        formatters={[
          inputFormatter.numberFormatter,
          inputFormatter.dpFormatter(dp),
          inputFormatter.currencyFormatter,
        ]}
        value={value}
        onValueChange={(e) => setValue(e)}
        helpText={error}
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
        }}
        autoComplete="off"
        onFocus={onFocus}
        onBlur={onBlur}
        onKeyDown={handleKeyDown}
        autoFocus
        color={open ? "danger" : undefined}
        classNames={{
          // TODO(theme): "oui-bg-base-700" is an invalid token (the base scale only defines 1-10) and generates no CSS. Remove it or replace with a valid token (e.g. "oui-bg-base-7") after visual comparison.
          root: "oui-bg-base-700 oui-px-2 oui-py-1 oui-rounded",
          input: "oui-pe-2",
        }}
        // prefix={
        //   <CloseIcon
        //     size={14}
        //     color="white"
        //     opacity={1}
        //     className="oui-cursor-pointer oui-opacity-50 hover:oui-opacity-100"
        //     onClick={(e) => {
        //       e.stopPropagation();
        //       e.preventDefault();
        //       onClose(e);
        //     }}
        //   />
        // }
        suffix={
          <button onClick={onClick}>
            <CheckIcon
              size={18}
              color="white"
              opacity={1}
              className="oui-cursor-pointer oui-opacity-50 hover:oui-opacity-100"
            />
          </button>
        }
      />
    </Tooltip>
  );
};

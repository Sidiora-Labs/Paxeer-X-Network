import { forwardRef, useMemo, useRef, useState } from "react";
import { Input, InputProps } from "../input";
import { Select } from "../../select";

export type QuantityInputProps = {
  tokens: string[];
} & Omit<InputProps, "onClear" | "suffix">;

//@ts-ignore
export const QuantityInput = forwardRef<HTMLInputElement, QuantityInputProps>(
  (props, ref) => {
    const { tokens, ...rest } = props;
    const [token, setToken] = useState(tokens[0]);
    const tokenOptions = useMemo(() => {
      return props.tokens.map((token) => ({
        name: token,
      }));
    }, [props.tokens]);

    const inputRef = useRef<HTMLInputElement>(null);

    return (
      <Input
        ref={(node) => {
          // @ts-ignore
          inputRef.current = node;
          if (ref) {
            if (typeof ref === "function") {
              ref(node);
            } else {
              ref.current = node;
            }
          }
        }}
        autoComplete="off"
        {...rest}
        suffix={
          <div className="oui-max-w-fit">
            <Select.tokens
              disabled={rest.disabled}
              variant="text"
              tokens={tokenOptions}
              value={token}
              size={rest.size}
              onValueChange={(value) => setToken(value)}
              // According to the design guideline, the token icon are not displayed when the text is right-aligned
              showIcon={rest.align !== "right"}
              contentProps={{
                align: "end",
                onCloseAutoFocus: (event) => {
                  event.preventDefault();
                  inputRef.current?.focus();
                },
              }}
            />
          </div>
        }
      />
    );
  }
);

QuantityInput.displayName = "QuantityInput";

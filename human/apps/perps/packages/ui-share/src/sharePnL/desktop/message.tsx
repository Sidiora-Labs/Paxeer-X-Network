import { FC, useRef, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { CloseCircleFillIcon, Input, toast } from "@paxeer-network/ui";
import { Checkbox } from "./checkbox";

export const Message: FC<{
  message: string;
  setMessage: any;
  check: boolean;
  setCheck: any;
}> = (props) => {
  const { message, setMessage, check, setCheck } = props;
  const [focus, setFocus] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const { t } = useTranslation();

  return (
    <div className="oui-mt-3 oui-mb-6 oui-flex oui-items-center">
      <Checkbox
        className="oui-mt-[2px]"
        checked={check}
        onCheckedChange={(e: boolean) => {
          setCheck(e);
        }}
      />
      <div
        className="oui-text-xs oui-text-base-contrast-54 oui-ms-1 hover:oui-cursor-pointer"
        onClick={() => {
          setCheck(!props.check);
        }}
      >
        {t("share.pnl.optionalInfo.message")}
      </div>
      {/* TODO(theme): "oui-bg-base-900" is an invalid token (the base scale only defines 1-10) and generates no CSS, so this wrapper has no background. Remove it or replace with "oui-bg-base-9" after visual comparison. */}
      <div className="oui-bg-base-900 oui-mx-2 oui-rounded-sm">
        <Input
          ref={inputRef}
          placeholder={t("share.pnl.optionalInfo.message.placeholder")}
          classNames={{
            root: "oui-w-[320px]",
          }}
          size="sm"
          value={message}
          autoFocus={false}
          suffix={
            focus && (
              <button
                className="oui-me-3 oui-cursor-pointer"
                onMouseDown={(e) => {
                  console.log("set message to empty");

                  setMessage("");
                  setTimeout(() => {
                    inputRef.current?.focus();
                  }, 50);
                  e.stopPropagation();
                }}
              >
                <CloseCircleFillIcon size={18} color="white" />
              </button>
            )
          }
          onFocus={() => setFocus(true)}
          onBlur={() => setFocus(false)}
          onChange={(e) => {
            if (e.target.value.length > 25) {
              toast.error(t("share.pnl.optionalInfo.message.maxLength"));
              return;
            }
            setCheck(e.target.value.length > 0);
            setMessage(e.target.value);
          }}
        />
      </div>
    </div>
  );
};

import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Text, cn } from "@paxeer-network/ui";
import { PnLDisplayFormat } from "../../types/types";

export const PnlFormatView: FC<{
  type: PnLDisplayFormat;
  curType?: PnLDisplayFormat;
  setPnlFormat: any;
}> = (props) => {
  const { type, curType, setPnlFormat } = props;
  const { t } = useTranslation();

  const text = useMemo(() => {
    switch (type) {
      case "roi_pnl":
        return t("share.pnl.displayFormat.roi&Pnl");
      case "roi":
        return t("share.pnl.displayFormat.roi");
      case "pnl":
        return t("share.pnl.displayFormat.pnl");
    }
  }, [type, t]);

  const isSelected = type === curType;

  let clsName = "oui-flex oui-items-center oui-gap-1 oui-cursor-pointer";
  if (isSelected) {
    clsName += " oui-text-base-contrast";
  } else {
    clsName += "";
  }

  return (
    <div
      className={clsName}
      onClick={() => {
        setPnlFormat(type);
      }}
    >
      <RadioButton sel={isSelected} />
      <Text
        size="xs"
        intensity={54}
        className={cn(
          "oui-ms-2 ",
          // isSelected && "oui-text-base-contrast"
        )}
      >
        {text}
      </Text>
    </div>
  );
};

const RadioButton = (props: {
  sel?: boolean;
  // onChange?: (sel: boolean) => void;
}) => {
  return (
    <button
      type="button"
      // onClick={(e) => {
      //   e.stopPropagation();
      // }}
    >
      {props.sel === true ? <SelIcon /> : <UnselIcon />}
    </button>
  );
};

const SelIcon = () => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-primary-darken"
    >
      <path
        className="oui-fill-base-contrast-36"
        d="M8.01 1.333a6.667 6.667 0 1 0 0 13.333 6.667 6.667 0 0 0 0-13.333m0 1.333a5.334 5.334 0 1 1-.001 10.667 5.334 5.334 0 0 1 0-10.667"
      />
      <circle cx="8" cy="8" r="3.333" />
    </svg>
  );
};

const UnselIcon = () => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-base-contrast-54"
    >
      <path
        d="M8.01 1.333a6.667 6.667 0 1 0 0 13.333 6.667 6.667 0 0 0 0-13.333m0 1.333a5.334 5.334 0 1 1-.001 10.667 5.334 5.334 0 0 1 0-10.667"
        fill="currentColor"
      />
    </svg>
  );
};

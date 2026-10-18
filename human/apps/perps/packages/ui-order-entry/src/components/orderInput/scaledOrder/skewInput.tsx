import { memo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { inputFormatter } from "@paxeer-network/ui";
import { InputType } from "../../../types";
import { CustomInput } from "../../customInput";
import { useOrderEntryContext } from "../../orderEntryContext";

type SkewInputProps = {
  skew?: string;
};

export const SkewInput = memo((props: SkewInputProps) => {
  const { t } = useTranslation();

  const { onFocus, onBlur, getErrorMsg, setOrderValue } =
    useOrderEntryContext();

  return (
    <CustomInput
      id="order_skew_input"
      label={t("orderEntry.skew")}
      className="oui-orderEntry-scaledOrder-skewInput"
      value={props.skew}
      error={getErrorMsg("skew")}
      onChange={(val) => {
        setOrderValue("skew", val);
      }}
      onFocus={onFocus(InputType.SKEW)}
      onBlur={onBlur(InputType.SKEW)}
      overrideFormatters={[
        inputFormatter.rangeFormatter({ min: 0, max: 100, dp: 2 }),
        inputFormatter.dpFormatter(2),
      ]}
      classNames={{
        root: "oui-rounded-b-xl",
      }}
    />
  );
});

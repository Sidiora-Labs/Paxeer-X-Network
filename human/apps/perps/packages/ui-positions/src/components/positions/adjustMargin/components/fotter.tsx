import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, Flex } from "@paxeer-network/ui";
import { AdjustMarginState } from "../adjustMargin.script";

export const Footer: FC<AdjustMarginState> = (props) => {
  const { t } = useTranslation();
  return (
    <Flex gap={3} className="oui-w-full oui-pt-5">
      <Button
        variant="contained"
        fullWidth
        size="lg"
        color="secondary"
        className="oui-h-10"
        onClick={props.close}
      >
        {t("common.cancel")}
      </Button>
      <Button
        variant="contained"
        color="primary"
        fullWidth
        size="lg"
        className="oui-h-10"
        loading={props.isLoading}
        disabled={!props.canConfirm}
        onClick={props.onConfirm}
      >
        {t("common.confirm")}
      </Button>
    </Flex>
  );
};

export default Footer;

import { FC, useMemo, ReactNode } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box } from "@paxeer-network/ui";

type NoticeProps = {
  message?: ReactNode;
  wrongNetwork?: boolean;
};

export const Notice: FC<NoticeProps> = (props) => {
  const { message, wrongNetwork } = props;
  const { t } = useTranslation();

  const content = useMemo(() => {
    if (wrongNetwork) {
      return t("connector.wrongNetwork.tooltip");
    }

    return message;
  }, [message, wrongNetwork, t]);

  if (content) {
    return (
      <Box
        mt={3}
        mb={3}
        className="oui-text-center oui-text-xs oui-text-warning-darken"
      >
        {content}
      </Box>
    );
  }

  return null;
};

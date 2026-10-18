import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, Flex } from "@paxeer-network/ui";

export const BottomButtons: FC<{
  onClickDownload: any;
  onClickCopy: any;
}> = (props) => {
  const { onClickDownload, onClickCopy } = props;
  const { t } = useTranslation();

  return (
    <Flex px={8} gap={3} mt={3} itemAlign={"center"}>
      <Button
        color={"secondary"}
        className="oui-flex-1 oui-flex oui-gap-1"
        onClick={onClickDownload}
      >
        <span>
          <DownloadIcon />
        </span>
        {t("common.download")}
      </Button>

      <Button className="oui-flex-1 oui-flex oui-gap-1" onClick={onClickCopy}>
        <span>
          <CopyIcon />
        </span>
        {t("common.copy")}
      </Button>
    </Flex>
  );
};

const DownloadIcon = () => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-base-contrast"
    >
      <path
        d="M4.66 1.994A2.667 2.667 0 0 0 1.995 4.66v6.666a2.667 2.667 0 0 0 2.667 2.667h6.666a2.667 2.667 0 0 0 2.667-2.667V4.661a2.667 2.667 0 0 0-2.667-2.667zM7.995 4.66c.368 0 .667.298.667.666V8.66h2l-2.667 2.666L5.328 8.66h2V5.327c0-.368.299-.667.667-.667"
        fill="currentColor"
      />
    </svg>
  );
};

const CopyIcon = () => {
  return (
    <svg
      width="17"
      height="16"
      viewBox="0 0 17 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-base-contrast"
    >
      <path
        d="M5.166 1.994A2.667 2.667 0 0 0 2.499 4.66v4a2.667 2.667 0 0 0 2.667 2.667 2.667 2.667 0 0 0 2.666 2.667h4a2.667 2.667 0 0 0 2.667-2.667v-4a2.667 2.667 0 0 0-2.667-2.667 2.667 2.667 0 0 0-2.666-2.666zm6.666 4c.737 0 1.334.596 1.334 1.333v4c0 .737-.597 1.334-1.334 1.334h-4A1.333 1.333 0 0 1 6.5 11.327h2.667a2.667 2.667 0 0 0 2.666-2.667z"
        fill="currentColor"
      />
    </svg>
  );
};

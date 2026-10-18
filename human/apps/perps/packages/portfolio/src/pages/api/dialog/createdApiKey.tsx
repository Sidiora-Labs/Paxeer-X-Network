import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Box,
  CopyIcon,
  Flex,
  SimpleDialog,
  Statistic,
  Text,
} from "@paxeer-network/ui";
import { ApiManagerScriptReturns } from "../apiManager.script";

export const CreatedAPIKeyDialog: FC<ApiManagerScriptReturns> = (props) => {
  const ip = props.generateKey?.ip ?? "--";
  const { t } = useTranslation();
  return (
    <SimpleDialog
      size="sm"
      open={props.showCreatedDialog}
      onOpenChange={(open) => {
        props.hideCreatedDialog?.();
      }}
      title={t("portfolio.apiKey.created")}
      actions={{
        primary: {
          label: t("common.ok"),
          "data-testid": "oui-testid-apiKey-createdApiKey-dialog-ok-btn",
          className:
            "oui-w-[120px] lg:oui-w-[154px] oui-bg-base-2 hover:oui-bg-base-3",
          size: "md",
          onClick: async () => {
            return props.doConfirm();
          },
        },
        secondary: {
          label: t("portfolio.apiKey.created.button.copyApiInfo"),
          "data-testid": "oui-testid-apiKey-createdApiKey-dialog-copy-btn",
          className:
            "oui-w-[120px] lg:oui-w-[154px] oui-bg-primary-darken hover:oui-opacity-80",
          size: "md",
          onClick: async () => {
            return props.onCopyApiKeyInfo();
          },
        },
      }}
      classNames={{
        footer: "oui-justify-center",
        content:
          "oui-bg-base-8 oui-w-[300px] lg:oui-w-[360px] oui-font-semibold",
        body: "oui-py-0 oui-pt-5",
      }}
    >
      <Flex direction={"column"} gap={4} itemAlign={"start"}>
        <Statistic label={t("common.accountId")}>
          <Text.formatted
            size="sm"
            intensity={80}
            copyable
            copyIconSize={16}
            className="oui-break-all"
            onCopy={props.onCopyAccountId}
            data-testid="oui-testid-apiKey-createdApiKey-dialog-key-span"
          >
            {props.accountId}
          </Text.formatted>
        </Statistic>
        <Statistic label={t("portfolio.apiKey.column.apiKey")}>
          <Text.formatted
            size="sm"
            intensity={80}
            copyable
            copyIconSize={16}
            className="oui-break-all"
            onCopy={() => props.onCopyApiKey(props.generateKey?.key)}
            data-testid="oui-testid-apiKey-createdApiKey-dialog-key-span"
          >
            {props.generateKey?.key}
          </Text.formatted>
        </Statistic>
        <Statistic label={t("portfolio.apiKey.secretKey")}>
          <Text.formatted
            size="sm"
            intensity={80}
            copyable
            copyIconSize={16}
            className="oui-break-all"
            onCopy={props.onCopyApiSecretKey}
          >
            {props.generateKey?.secretKey}
          </Text.formatted>{" "}
        </Statistic>
        <Statistic label={t("portfolio.apiKey.ip")}>
          <Flex
            width={320}
            gap={1}
            itemAlign={"center"}
            className="oui-text-base-contrast-80 oui-text-sm"
          >
            <Box className="oui-max-h-[100px] oui-flex-1 oui-overflow-hidden oui-text-ellipsis oui-line-clamp-5 oui-break-all">
              {ip}
            </Box>
            {ip !== "--" && (
              <Box
                width={16}
                height={16}
                className="oui-cursor-pointer oui-flex-shrink-0"
              >
                <CopyIcon
                  color="white"
                  opacity={0.54}
                  size={16}
                  onClick={(e) => {
                    if (props.generateKey?.ip)
                      navigator.clipboard.writeText(props.generateKey?.ip);
                    props?.onCopyIP();
                  }}
                />
              </Box>
            )}
          </Flex>
        </Statistic>
        <Statistic label={t("portfolio.apiKey.permissions")}>
          <Text
            size="sm"
            intensity={80}
            data-testid="oui-testid-apiKey-createdApiKey-dialog-permissions-span"
          >
            {props.generateKey?.permissions}
          </Text>
        </Statistic>
        <div></div>
        <Text color="warning" size="xs" className="oui-text-center">
          {t("portfolio.apiKey.created.warning")}
        </Text>
      </Flex>
    </SimpleDialog>
  );
};

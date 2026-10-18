import { FC, useEffect, useState } from "react";
import { APIKeyItem } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, SimpleDialog, Statistic, Text } from "@paxeer-network/ui";
import { Checkbox } from "./createApiKey";

export const EditAPIKeyDialog: FC<{
  item: APIKeyItem;
  open: boolean;
  setOpen?: any;
  onUpdate?: (item: APIKeyItem, ip?: string) => Promise<void>;
  verifyIP: (ip: string) => string;
}> = (props) => {
  const { item, open, setOpen, onUpdate } = props;
  const [ipText, setIpText] = useState(item.ip_restriction_list?.join(","));
  const [read, setRead] = useState(true);
  const [trade, setTrade] = useState(true);
  const [hint, setHint] = useState("");
  const { t } = useTranslation();

  useEffect(() => {
    setIpText(item.ip_restriction_list.join(","));
    const scope = item.scope?.toLocaleLowerCase() || "";

    setRead(scope.includes("read"));
    setTrade(scope.includes("trading"));
  }, [item]);

  useEffect(() => {
    if (ipText.length === 0) setHint("");
  }, [ipText]);

  return (
    <SimpleDialog
      size="sm"
      open={open}
      onOpenChange={setOpen}
      title={t("portfolio.apiKey.edit.dialog.title")}
      actions={{
        primary: {
          label: t("common.confirm"),
          "data-testid": "oui-testid-apiKey-editApiKey-dialog-confirm-btn",
          className: "oui-w-[120px] lg:oui-w-[154px]",
          onClick: async () => {
            if (ipText.length > 0) {
              const hint = props.verifyIP(ipText);
              setHint(hint);
              if (hint.length > 0) {
                return;
              }
            }
            await props.onUpdate?.(item, ipText);
            setOpen(false);
          },
          disabled: item.ip_restriction_list.join(",") === ipText,
          size: "md",
          fullWidth: true,
        },
      }}
      classNames={{
        footer: "oui-justify-center",
        content:
          "oui-bg-base-8 oui-w-[300px] lg:oui-w-[360px] oui-font-semibold",
      }}
    >
      <Flex direction={"column"} gap={6}>
        {/* <TextField label={"IP restriction (optional)"} rows={5} className="oui-w-full oui-h-auto" classNames={{
          input: "oui-h-[100px]",
          root: "oui-h-[100px]"
        }}/> */}
        <Flex direction={"column"} gap={1} width={"100%"} itemAlign={"start"}>
          <Text intensity={54} size="2xs">
            {t("portfolio.apiKey.create.ipRestriction")}
          </Text>
          <textarea
            data-testid="oui-testid-apiKey-editApiKey-dialog-textarea"
            placeholder={t("portfolio.apiKey.create.ipRestriction.placeholder")}
            className={cn(
              "oui-h-[100px] oui-w-full oui-rounded-xl oui-bg-base-6 oui-p-3 oui-text-sm oui-text-base-contrast-80",
              "oui-border-0 oui-outline-none focus:oui-border-2 focus:oui-border-primary-darken",
              "oui-placeholder-base-contrast-20",
              hint.length > 0 &&
                "oui-outline-1 oui-outline-danger focus:oui-outline-none",
            )}
            value={ipText}
            onChange={(e) => {
              setIpText(e.target.value);
            }}
            style={{
              resize: "none",
            }}
          />
          {hint.length > 0 && (
            <Flex gap={1} className="oui-relative">
              <div
                className={cn(
                  "oui-absolute oui-top-[10px]",
                  "oui-size-1 oui-rounded-full oui-bg-danger",
                )}
              />
              <Text color="danger" size="xs" className="oui-ms-2">
                {hint}
              </Text>
            </Flex>
          )}
        </Flex>
        <Statistic
          label={
            <Text size="xs" intensity={54}>
              {t("portfolio.apiKey.permissions")}
            </Text>
          }
          className="oui-w-full"
        >
          <Flex
            direction={"row"}
            gap={6}
            itemAlign={"start"}
            className="oui-mt-2"
          >
            <Checkbox
              disabled
              size={18}
              checked={read}
              onCheckedChange={(e) => setRead(e as boolean)}
              label={t("portfolio.apiKey.permissions.read")}
            />
            <Checkbox
              disabled
              size={18}
              checked={trade}
              onCheckedChange={(e) => setTrade(e as boolean)}
              label={t("portfolio.apiKey.permissions.trading")}
            />
          </Flex>
        </Statistic>
      </Flex>
    </SimpleDialog>
  );
};

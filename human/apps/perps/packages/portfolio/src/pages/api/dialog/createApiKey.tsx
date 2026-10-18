import { FC, useEffect, useState } from "react";
import { ScopeType } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, SimpleDialog, Statistic, Text } from "@paxeer-network/ui";
import { ApiManagerScriptReturns } from "../apiManager.script";

export const CreateAPIKeyDialog: FC<ApiManagerScriptReturns> = (props) => {
  const [ipText, setIpText] = useState("");
  const [read, setRead] = useState(true);
  const [trade, setTrade] = useState(true);
  const [hint, setHint] = useState("");
  const { t } = useTranslation();

  useEffect(() => {
    if (!props.showCreateDialog) {
      setIpText("");
      setRead(true);
      setTrade(true);
      setHint("");
    }
  }, [props.showCreateDialog]);

  useEffect(() => {
    if (ipText.length === 0) setHint("");
  }, [ipText]);

  return (
    <SimpleDialog
      size="sm"
      open={props.showCreateDialog}
      onOpenChange={(open) => {
        props.hideCreateDialog?.();
      }}
      title={t("portfolio.apiKey.create.dialog.title")}
      actions={{
        primary: {
          label: t("common.confirm"),
          className: "oui-w-[120px] lg:oui-w-[154px]",
          "data-testid": "oui-testid-apiKey-createApiKey-dialog-comfirm-btn",
          onClick: async () => {
            if (ipText.length > 0) {
              const hint = props.verifyIP(ipText);
              setHint(hint);
              if (hint.length > 0) {
                return;
              }
            }

            const scopes: string[] = [];
            if (read) {
              scopes.push("read");
            }
            if (trade) {
              scopes.push("trading");
            }
            await props.doCreate(ipText, scopes.join(",") as ScopeType);
          },
          disabled: !trade && !read,
          size: "md",
        },
      }}
      classNames={{
        footer: "oui-justify-center",
        content:
          "oui-bg-base-8 oui-w-[300px] lg:oui-w-[360px] oui-font-semibold",
      }}
    >
      <Flex direction={"column"} gap={6}>
        <Flex direction={"column"} gap={1} width={"100%"} itemAlign={"start"}>
          <Text intensity={54} size="2xs">
            {t("portfolio.apiKey.create.ipRestriction")}
          </Text>
          <textarea
            data-testid="oui-testid-apiKey-createApiKey-dialog-textarea"
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
              size={18}
              checked={read}
              onCheckedChange={(e) => setRead(e as boolean)}
              label={t("portfolio.apiKey.permissions.read")}
              testid="oui-testid-apiKey-createApiKey-dialog-read-checkbox"
            />
            <Checkbox
              size={18}
              checked={trade}
              onCheckedChange={(e) => setTrade(e as boolean)}
              label={t("portfolio.apiKey.permissions.trading")}
              testid="oui-testid-apiKey-createApiKey-dialog-trading-checkbox"
            />
          </Flex>
        </Statistic>
      </Flex>
    </SimpleDialog>
  );
};

export const Checkbox: FC<{
  size?: number;
  checked: boolean;
  onCheckedChange: (checked?: boolean) => void;
  disabled?: boolean;
  label: string;
  testid?: string;
}> = (props) => {
  return (
    <button
      disabled={props.disabled}
      onClick={(e) => {
        props.onCheckedChange(!props.checked);
      }}
      className={
        "oui-flex oui-items-center oui-gap-2 disabled:oui-cursor-not-allowed disabled:oui-opacity-50"
      }
      data-testid={props.testid}
    >
      {props.checked ? (
        <svg
          width={"props.size"}
          height={props.size}
          viewBox="0 0 24 24"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
        >
          <path
            fillRule="evenodd"
            clipRule="evenodd"
            d="M6.99072 2.92981C4.78172 2.92981 2.99072 4.72071 2.99072 6.92981V16.9298C2.99072 19.1389 4.78172 20.9298 6.99072 20.9298H16.9907C19.1997 20.9298 20.9907 19.1389 20.9907 16.9298V6.92981C20.9907 4.72071 19.1997 2.92981 16.9907 2.92981H6.99072ZM16.9853 7.31211C17.2125 7.09537 17.5236 7 17.8218 7C18.1201 7 18.4312 7.09537 18.6584 7.31211C19.1139 7.74546 19.1139 8.47384 18.6584 8.9072L10.5077 16.675C10.0534 17.1083 9.28909 17.1083 8.83472 16.675L5.34077 13.3459C4.88641 12.9126 4.88641 12.1841 5.34077 11.7508C5.79631 11.3175 6.56057 11.3175 7.01493 11.7508L9.67122 14.2822L16.9853 7.31211Z"
            className="oui-fill-base-contrast-54"
          />
        </svg>
      ) : (
        <svg
          width={props.size}
          height={props.size}
          viewBox="0 0 24 24"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
        >
          <path
            d="M6.99072 2.92981C4.78172 2.92981 2.99072 4.72071 2.99072 6.92981V16.9298C2.99072 19.1389 4.78172 20.9298 6.99072 20.9298H16.9907C19.1997 20.9298 20.9907 19.1389 20.9907 16.9298V6.92981C20.9907 4.72071 19.1997 2.92981 16.9907 2.92981H6.99072ZM6.99072 4.92981H16.9907C18.0957 4.92981 18.9907 5.82521 18.9907 6.92981V16.9298C18.9907 18.0344 18.0957 18.9298 16.9907 18.9298H6.99072C5.88572 18.9298 4.99072 18.0344 4.99072 16.9298V6.92981C4.99072 5.82521 5.88572 4.92981 6.99072 4.92981Z"
            className="oui-fill-base-contrast-54"
          />
        </svg>
      )}

      <Text
        intensity={54}
        size="sm"
        className="oui-whitespace-nowrap oui-break-normal"
      >
        {props.label}
      </Text>
    </button>
  );
};

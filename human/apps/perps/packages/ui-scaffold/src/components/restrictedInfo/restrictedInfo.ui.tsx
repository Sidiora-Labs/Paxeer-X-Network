import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Checkbox, cn, Flex, SimpleDialog } from "@paxeer-network/ui";
import { InfoIcon } from "../icons";
import type { UseRestrictedInfoScriptReturn } from "./restrictedInfo.script";

export type RestrictedInfoProps = UseRestrictedInfoScriptReturn & {
  className?: string;
};

export const RestrictedInfo: FC<RestrictedInfoProps> = (props) => {
  const { brokerName, agree, setAgree } = props;
  const {
    ip,
    content,
    restrictedOpen,
    canUnblock,
    accessRestricted,
    setAccessRestricted,
  } = props.restrictedInfo || {};
  const { t } = useTranslation();

  // if user region is in the canUnblock regions and accessRestricted is not set, show the dialog
  if (restrictedOpen && canUnblock && accessRestricted === undefined) {
    return (
      <SimpleDialog
        open={canUnblock}
        title={t("restrictedInfo.accessRestricted")}
        size="sm"
        closable={false}
        actions={{
          secondary: {
            label: t("common.cancel"),
            onClick: () => {
              setAccessRestricted(true);
            },
            size: "md",
          },
          primary: {
            label: t("common.confirm"),
            onClick: async () => {
              setAccessRestricted(false);
            },
            size: "md",
            disabled: !agree,
          },
        }}
      >
        {t("restrictedInfo.accessRestricted.description")}

        <Flex gapX={1} pt={2}>
          <Checkbox
            id="orderConfirm"
            color={"white"}
            checked={agree}
            onCheckedChange={(checked) => {
              setAgree(!!checked);
            }}
          />
          <label htmlFor="orderConfirm" className="oui-cursor-pointer">
            {t("restrictedInfo.accessRestricted.agree")}
          </label>
        </Flex>
      </SimpleDialog>
    );
  }

  if (!restrictedOpen) {
    return;
  }

  const renderContent = () => {
    if (typeof content === "function") {
      return <span>{content({ ip: ip!, brokerName })}</span>;
    }
    return (
      content || (
        <span>
          {t("restrictedInfo.description.default", {
            brokerName,
            ip,
          })}
        </span>
      )
    );
  };

  return (
    <Flex
      ref={props.container}
      justify="center"
      className={cn(
        "oui-restricted-info",
        "oui-rounded-xl oui-p-[7px]",
        "oui-text-warning-darken",
        props.className,
      )}
    >
      <Flex
        className={cn(
          "oui-min-h-[20px] oui-gap-1",
          "oui-text-2xs oui-leading-4 md:oui-text-sm",
          "oui-items-start lg:oui-justify-center",
          props.mutiLine ? "lg:oui-items-start" : "lg:oui-items-center",
        )}
      >
        <InfoIcon size={20} className="oui-size-4 oui-shrink-0 lg:oui-size-5" />
        {renderContent()}
      </Flex>
    </Flex>
  );
};

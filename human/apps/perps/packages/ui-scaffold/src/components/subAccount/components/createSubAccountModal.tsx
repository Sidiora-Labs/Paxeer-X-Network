import { useState, useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import {
  Flex,
  SimpleDialog,
  toast,
  Text,
  useScreen,
  cn,
  Tooltip,
} from "@paxeer-network/ui";
import { AddIcon } from "../icons";
import { NickNameTextField } from "./common";

const MAX_SUB_ACCOUNT_COUNT = 10;
type CreateSubAccountProps = {
  create: (nickName: string) => Promise<void>;
};

export const CreateSubAccount = (props: CreateSubAccountProps) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const [open, setOpen] = useState(false);
  const [nickName, setNickName] = useState<string | undefined>(undefined);
  const { state } = useAccount();
  const [invalid, setInvalid] = useState(false);
  const [loading, setLoading] = useState(false);

  const { widgetConfigs } = useAppContext();

  const maxSubAccountCount = useMemo(() => {
    return (
      widgetConfigs?.subAccount?.maxSubAccountCount ?? MAX_SUB_ACCOUNT_COUNT
    );
  }, [widgetConfigs]);

  const subAccountCount = useMemo(() => {
    return state.subAccounts?.length ?? 0;
  }, [state]);

  const trigger = useMemo(() => {
    return subAccountCount >= maxSubAccountCount ? (
      <Tooltip
        className="oui-max-w-[188px]"
        content={t("subAccount.modal.create.max.description")}
      >
        <AddIcon
          className={cn("oui-cursor-not-allowed oui-fill-base-contrast-20")}
        />
      </Tooltip>
    ) : (
      <AddIcon
        className={cn(
          "oui-cursor-pointer oui-fill-base-contrast-54 hover:oui-fill-base-contrast",
        )}
        onClick={() => {
          setOpen(true);
        }}
      />
    );
  }, [subAccountCount, maxSubAccountCount, t]);

  const header = (
    <Flex
      py={3}
      direction="column"
      justify="between"
      itemAlign="start"
      width="100%"
    >
      <Text weight="semibold">{t("subAccount.modal.create.title")}</Text>
      <Text className="oui-text-2xs oui-text-base-contrast-36">
        {t("subAccount.modal.create.description", {
          subAccountCount,
          remainingCount: maxSubAccountCount - subAccountCount,
        })}
      </Text>
    </Flex>
  );

  const reset = () => {
    setNickName("");
    setInvalid(false);
    setLoading(false);
  };

  const validateNickName = (nickName: string | undefined) => {
    if (!nickName || !(nickName.length >= 1 && nickName.length <= 20)) {
      setInvalid(true);
      return true;
    }
    setInvalid(false);
    return false;
  };

  const doCreatSubAccount = (nickName: string | undefined) => {
    let _nickName = `Sub-account ${subAccountCount + 1}`;
    if (nickName) {
      _nickName = nickName.trim();
    }

    setLoading(true);
    props
      .create(_nickName)
      .then((res) => {
        console.log("res", res);
        reset();
        toast.success(t("subAccount.modal.create.success.description"));

        setOpen(false);
      })
      .catch((e: any) => {
        toast.error(t("subAccount.modal.create.failed.description"));
      })
      .finally(() => {
        setLoading(false);
      });
  };
  return (
    <>
      {trigger}
      <SimpleDialog
        title={header}
        open={open}
        onOpenChange={(open) => {
          reset();
          setOpen(open);
        }}
        size={isMobile ? "sm" : "xl"}
        actions={{
          primary: {
            label: t("common.confirm"),
            disabled: invalid || loading,
            loading: loading,
            onClick: () => {
              const invalid = validateNickName(nickName);
              if (invalid) {
                return;
              }
              doCreatSubAccount(nickName);
            },
          },
        }}
        classNames={{
          content: "oui-w-[360px]",
        }}
      >
        <NickNameTextField
          nickName={nickName}
          setNickName={(nickName) => {
            validateNickName(nickName);
            setNickName(nickName);
          }}
          subAccountCount={subAccountCount}
          invalid={invalid}
        />
      </SimpleDialog>
    </>
  );
};

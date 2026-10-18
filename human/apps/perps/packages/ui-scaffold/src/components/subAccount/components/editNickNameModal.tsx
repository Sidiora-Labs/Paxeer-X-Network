import { useEffect, useState } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { SimpleDialog, toast, Text, TextField } from "@paxeer-network/ui";
import { NickNameDescriptionText, NickNameTextField } from "./common";

export const EditNickNameDialog = (props: {
  nickName: string;
  open: boolean;
  accountId: string;
  onOpenChange: (open: boolean) => void;
}) => {
  const { subAccount } = useAccount();
  const [loading, setLoading] = useState(false);
  const { t } = useTranslation();
  const [newNickName, setNewNickName] = useState<string | undefined>(undefined);
  const [invalid, setInvalid] = useState(false);

  const validateNickName = (nickName: string | undefined) => {
    if (!nickName || !(nickName.length >= 1 && nickName.length <= 20)) {
      setInvalid(true);
      return true;
    }
    setInvalid(false);
  };
  useEffect(() => {
    setNewNickName(props.nickName);
    setInvalid(false);
    setLoading(false);
  }, [props.nickName, props.open]);
  return (
    <SimpleDialog
      title={<Text>{t("subAccount.modal.edit.title")}</Text>}
      open={props.open}
      onOpenChange={props.onOpenChange}
      classNames={{
        content: "oui-w-[360px]",
      }}
      actions={{
        primary: {
          label: t("common.confirm"),
          disabled: loading || invalid,
          loading: loading,
          onClick: () => {
            if (validateNickName(newNickName)) {
              return;
            }
            setLoading(true);
            subAccount
              ?.update({
                subAccountId: props.accountId,
                description: newNickName,
              })
              .catch((e) => {
                console.log("e", e);
                toast.error(t("subAccount.modal.edit.failed.description"));
              })
              .then((res) => {
                toast.success(t("subAccount.modal.edit.success.description"));
                props.onOpenChange(false);
              })
              .finally(() => {
                setLoading(false);
              });
          },
        },
        secondary: {
          label: t("common.cancel"),
          onClick: () => props.onOpenChange(false),
        },
      }}
    >
      <NickNameTextField
        nickName={newNickName}
        setNickName={(nickName) => {
          validateNickName(nickName);
          setNewNickName(nickName ?? "");
        }}
        invalid={invalid}
      />
    </SimpleDialog>
  );
};

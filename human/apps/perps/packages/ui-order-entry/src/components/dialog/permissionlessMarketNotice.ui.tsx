import { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { i18n } from "@paxeer-network/i18n";
import {
  Button,
  Checkbox,
  Flex,
  registerSimpleDialog,
  Text,
  textVariants,
} from "@paxeer-network/ui";

export type PermissionlessMarketNoticeDialogProps = {
  onConfirm: () => void;
  onCancel: () => void;
};

export const PermissionlessMarketNoticeDialog: FC<
  PermissionlessMarketNoticeDialogProps
> = (props) => {
  const { onConfirm, onCancel } = props;
  const { t } = useTranslation();
  const [checked, setChecked] = useState(false);

  const content = (
    <Flex
      direction="column"
      gap={3}
      itemAlign={"start"}
      className="oui-permissionlessNotice-content"
    >
      <Text
        className={textVariants({
          size: "sm",
          intensity: 54,
        })}
      >
        {t("orderEntry.permissionlessNotice.content1")}
      </Text>
      <Text
        className={textVariants({
          size: "sm",
          intensity: 54,
        })}
      >
        {t("orderEntry.permissionlessNotice.content2")}
      </Text>
      <Text
        className={textVariants({
          size: "sm",
          intensity: 54,
        })}
      >
        {t("orderEntry.permissionlessNotice.content3")}
      </Text>
    </Flex>
  );

  const checkboxSection = (
    <Flex
      gapX={1}
      pt={4}
      pb={5}
      itemAlign="start"
      className="oui-permissionlessNotice-checkbox oui-orderEntry-orderConfirmDialog-disableConfirm oui-cursor-pointer"
    >
      <Checkbox
        id="permissionlessNotice"
        color="white"
        className="oui-permissionlessNotice-checkbox-input oui-mt-1"
        checked={checked}
        onCheckedChange={(value) => setChecked(!!value)}
      />
      <label
        htmlFor="permissionlessNotice"
        className={textVariants({
          size: "xs",
          intensity: 54,
          className: "oui-cursor-pointer",
        })}
      >
        {t("orderEntry.permissionlessNotice.checkbox")}
      </label>
    </Flex>
  );

  const confirmButton = (
    <Flex
      className="oui-permissionlessNotice-confirm-button oui-w-full"
      justify={"center"}
    >
      <Button
        fullWidth
        size="md"
        className="oui-permissionlessNotice-confirm oui-confirm-btn oui-max-w-[244px]"
        disabled={!checked}
        onClick={() => onConfirm()}
      >
        {t("common.confirm")}
      </Button>
    </Flex>
  );

  return (
    <>
      {content}
      {checkboxSection}
      {confirmButton}
    </>
  );
};

PermissionlessMarketNoticeDialog.displayName =
  "PermissionlessMarketNoticeDialog";

const Dialog = (
  props: Omit<
    PermissionlessMarketNoticeDialogProps,
    "onCancel" | "onConfirm"
  > & {
    close: () => void;
    resolve: (value?: unknown) => void;
    reject: (reason?: unknown) => void;
  },
) => {
  const { close, resolve, reject } = props;

  return (
    <PermissionlessMarketNoticeDialog
      onCancel={() => {
        reject();
        close();
      }}
      onConfirm={() => {
        resolve();
        close();
      }}
    />
  );
};

export const permissionlessMarketNoticeDialogId = "permissionlessMarketNotice";
export const permissionlessMarketNoticeDesktopDialogId =
  "permissionlessMarketNoticeDesktop";

registerSimpleDialog(permissionlessMarketNoticeDialogId, Dialog, {
  size: "sm",
  title: () => i18n.t("orderEntry.permissionlessNotice.title"),
});

registerSimpleDialog(permissionlessMarketNoticeDesktopDialogId, Dialog, {
  size: "xl",
  title: () => i18n.t("orderEntry.permissionlessNotice.title"),
});

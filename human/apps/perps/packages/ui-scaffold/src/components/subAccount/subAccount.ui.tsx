import { useCallback, useRef, useMemo, useState, ReactNode } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  SimpleDialog,
  useScreen,
  Text,
  Flex,
  ScrollArea,
  modal,
  cn,
  DropdownMenuRoot,
  DropdownMenuTrigger,
  DropdownMenuPortal,
  DropdownMenuContent,
  PopoverRoot,
  PopoverContent,
  PopoverAnchor,
} from "@paxeer-network/ui";
import { AccountItem } from "./components/accountItem";
import { CreateSubAccount } from "./components/createSubAccountModal";
import { EditNickNameDialog } from "./components/editNickNameModal";
import { SubAccountIcon, SwapIcon } from "./icons";
import { SubAccountScriptReturn } from "./subAccount.script";

export function SubAccountUI(
  props: SubAccountScriptReturn & { customTrigger?: ReactNode },
) {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const onMouseEnter = useCallback(() => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    // setOpen(true);
  }, []);
  const header = <Text weight="semibold">{t("subAccount.modal.title")}</Text>;
  const trigger = useMemo(() => {
    if (props.customTrigger && isMobile) {
      return (
        <div onClick={() => props.onOpenChange(true)}>
          {props.customTrigger}
        </div>
      );
    }
    if (isMobile) {
      return (
        <Flex
          className="oui-size-8 oui-rounded-md oui-bg-base-6"
          itemAlign="center"
          justify="center"
        >
          <SubAccountIcon
            className={cn("oui-cursor-pointer")}
            onClick={() => props.onOpenChange(true)}
          />
        </Flex>
      );
    }
    return <SubAccountIcon className={cn("oui-cursor-pointer")} />;
  }, [isMobile, props.customTrigger]);

  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [editAccountItem, setEditAccountItem] = useState<
    | {
        accountId: string;
        description: string;
      }
    | undefined
  >(undefined);

  const noSubAccount = (
    <Flex
      direction="column"
      itemAlign="start"
      justify="center"
      px={5}
      width="100%"
      className="oui-h-[120px]"
    >
      <Text className="oui-text-center oui-text-xs oui-font-semibold oui-text-base-contrast-36">
        {t("subAccount.modal.noAccount.description")}
      </Text>
    </Flex>
  );

  const renderSubAccount = () => {
    if (!props.subAccounts?.length) {
      return noSubAccount;
    }
    return (
      <ScrollArea className="oui-custom-scrollbar oui-max-h-[205px] oui-w-full oui-overflow-y-auto">
        <Flex direction="column" gap={2} itemAlign="start" width="100%">
          {props.subAccounts.map((subAccount) => (
            <AccountItem
              key={subAccount.id}
              accountId={subAccount.id}
              description={subAccount.description}
              isMainAccount={false}
              isCurrent={subAccount.id === props.currentAccountId}
              onSwitch={(accountId) => {
                props.onSwitch?.(accountId);
              }}
              accountValue={subAccount.accountValue ?? 0}
              onEdit={() => {
                setEditAccountItem({
                  accountId: subAccount.id,
                  description: subAccount.description ?? "",
                });
                setEditDialogOpen(true);
              }}
            />
          ))}
        </Flex>
      </ScrollArea>
    );
  };

  const content = (
    <>
      <Flex direction="column" gap={5} itemAlign="start" width="100%">
        {!isMobile && header}
        <Flex direction="column" gap={5} itemAlign="start" width="100%">
          <Flex direction="column" gap={2} itemAlign="start" width="100%">
            <Text className="oui-text-xs oui-leading-3 oui-text-base-contrast-54">
              {t("subAccount.modal.mainAccount.title")}
            </Text>
            <AccountItem
              accountId={props.mainAccount?.id ?? ""}
              isMainAccount={true}
              userAddress={props.userAddress ?? ""}
              isCurrent={props.currentAccountId === props.mainAccount?.id}
              onSwitch={(accountId) => {
                props.onSwitch?.(accountId);
              }}
              accountValue={props.mainAccount?.accountValue ?? 0}
            />
          </Flex>

          <Flex direction="column" gap={2} itemAlign="start" width="100%">
            <Flex justify="between" itemAlign="center" width="100%" gap={2}>
              <Text className="oui-text-xs oui-leading-3 oui-text-base-contrast-54">
                {t("subAccount.modal.subAccounts.title")}
              </Text>
              <Flex justify="end" gap={2}>
                {props.subAccounts.length > 0 && (
                  <SwapIcon
                    className="oui-cursor-pointer oui-fill-base-contrast-54 hover:oui-fill-base-contrast"
                    onClick={() => {
                      // todo need show transfer modal
                      let dialogId = "TransferDialogId";
                      if (isMobile) {
                        dialogId = "TransferSheetId";
                      }
                      modal.show(dialogId);
                    }}
                  />
                )}
                <CreateSubAccount create={props.createSubAccount} />
              </Flex>
            </Flex>
            {renderSubAccount()}
          </Flex>
        </Flex>
      </Flex>
      <EditNickNameDialog
        accountId={editAccountItem?.accountId ?? ""}
        nickName={editAccountItem?.description ?? ""}
        open={editDialogOpen}
        onOpenChange={setEditDialogOpen}
      />
    </>
  );
  if (isMobile) {
    return (
      <>
        {trigger}
        <SimpleDialog
          title={header}
          open={props.open}
          onOpenChange={props.onOpenChange}
          size="xs"
          classNames={{
            content: "oui-w-[300px]",
          }}
        >
          {content}
        </SimpleDialog>
      </>
    );
  }

  return (
    <PopoverRoot open={props.open} onOpenChange={props.onOpenChange}>
      <PopoverAnchor>
        <div
          onMouseEnter={() => {
            props.onOpenChange(true);
          }}
          onMouseLeave={() => {
            timer.current = setTimeout(() => {
              props.onOpenChange(false);
            }, 150);
          }}
        >
          {trigger}
        </div>
      </PopoverAnchor>
      <PopoverContent
        align="start"
        side="bottom"
        sideOffset={16}
        collisionPadding={{ left: 16, right: 16 }}
        onMouseEnter={onMouseEnter}
        onMouseLeave={() => {
          props.onOpenChange(false);
          timer.current ? clearTimeout(timer.current) : void 0;
        }}
        className={cn(
          "oui-w-[300px] oui-space-y-[2px] oui-border oui-border-line-6 oui-p-5",
        )}
      >
        {content}
      </PopoverContent>
    </PopoverRoot>
  );
}

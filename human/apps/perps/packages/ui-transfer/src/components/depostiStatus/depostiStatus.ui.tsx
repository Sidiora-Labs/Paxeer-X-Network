import React, { FC, ReactNode, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AssetHistoryStatusEnum } from "@paxeer-network/types";
import { Flex, cn, Text, CloseIcon } from "@paxeer-network/ui";
import { DepositStatusScriptReturn } from "./depostiStatus.script";

export type DepositStatusProps = {
  className?: string;
  classNames?: {
    root?: string;
    items?: string;
  };
  onClick?: () => void;
} & DepositStatusScriptReturn;

export const DepositStatus: React.FC<DepositStatusProps> = (props) => {
  const { canTrade, className, classNames, pendingCount, completedCount } =
    props;

  if (!canTrade || (pendingCount === 0 && completedCount === 0)) {
    return null;
  }

  return (
    <Flex
      direction="column"
      gapY={1}
      width="100%"
      className={cn(className, classNames?.root)}
    >
      <DepositStatusContent
        className={classNames?.items}
        count={pendingCount}
        status={AssetHistoryStatusEnum.PENDING}
        onClick={props.onClick}
        estimatedTime={props.estimatedTime}
        onClose={props.onClose}
      />
      <DepositStatusContent
        className={classNames?.items}
        count={completedCount}
        status={AssetHistoryStatusEnum.COMPLETED}
        onClick={props.onClick}
        onClose={props.onClose}
      />
    </Flex>
  );
};

type DepositStatusContentProps = {
  count: number;
  status: AssetHistoryStatusEnum.PENDING | AssetHistoryStatusEnum.COMPLETED;
  onClick?: () => void;
  className?: string;
  estimatedTime?: string | number;
  onClose: (status: AssetHistoryStatusEnum) => void;
};

export const DepositStatusContent = (props: DepositStatusContentProps) => {
  const { t } = useTranslation();
  const { count, status, estimatedTime } = props;

  const content = useMemo(() => {
    if (status === AssetHistoryStatusEnum.PENDING) {
      return count > 1
        ? t("transfer.deposit.status.pending.multiple")
        : t("transfer.deposit.status.pending.one");
    }

    return count > 1
      ? t("transfer.deposit.status.completed.multiple")
      : t("transfer.deposit.status.completed.one");
  }, [t, status, count]);

  if (count === 0) {
    return null;
  }

  const clickable = typeof props.onClick === "function";

  const renderContent = () => {
    if (
      status === AssetHistoryStatusEnum.PENDING &&
      count === 1 &&
      estimatedTime
    ) {
      return (
        <Text size="2xs" color="primary">
          ~ {estimatedTime}
        </Text>
      );
    }

    return <Badge>{props.count}</Badge>;
  };

  return (
    <Flex
      intensity={900}
      gapX={1}
      width="100%"
      r="full"
      className={cn(
        "oui-px-3 lg:oui-px-0",
        "oui-py-2 lg:oui-py-0",
        props.className,
      )}
    >
      <Flex
        justify="between"
        itemAlign="center"
        className={cn(
          "oui-font-normal",
          "oui-text-base-contrast-80 hover:oui-text-base-contrast",
          clickable && "oui-cursor-pointer",
        )}
        width="100%"
        onClick={props.onClick}
      >
        <Flex gapX={1}>
          <Dot
            className={
              status === AssetHistoryStatusEnum.COMPLETED
                ? "oui-bg-success"
                : "oui-bg-primary"
            }
          />
          <Text size="2xs">{content}</Text>
        </Flex>
        {renderContent()}
      </Flex>
      <CloseIcon
        size={14}
        opacity={1}
        className="oui-text-base-contrast-54 hover:oui-text-base-contrast"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          props.onClose(status);
        }}
      />
    </Flex>
  );
};

const Badge: FC<{ className?: string; children: ReactNode }> = (props) => {
  return (
    <Flex
      justify="center"
      itemAlign="center"
      r="full"
      width={18}
      height={18}
      className={cn("oui-bg-line-12", props.className)}
    >
      <Text size="3xs" intensity={80}>
        {props.children}
      </Text>
    </Flex>
  );
};

const Dot: FC<{ className?: string }> = (props) => {
  return (
    <div
      className={cn(
        "oui-mx-[5px] oui-size-2 oui-shrink-0 oui-rounded-full",
        props.className,
      )}
    />
  );
};

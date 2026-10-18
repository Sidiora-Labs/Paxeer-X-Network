import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Box,
  Button,
  CheckedCircleFillIcon,
  CloseCircleFillIcon,
  cn,
  Divider,
  Flex,
  Spinner,
  Text,
} from "@paxeer-network/ui";
import { TopRightArrowIcon } from "../icons";
import { SwapMode, SwapProcessStatus } from "../types";

type ProcessStatusProps = {
  status?: SwapProcessStatus;
  mode: SwapMode;
  statusUrl?: string;
  onComplete?: (isSuccss: boolean) => void;
  brokerName?: string;
};

enum SwapState {
  Pending = "pending",
  Failed = "failed",
  Success = "success",
}

type TProcessItem = {
  title: string;
  description: string;
  state?: SwapState;
};

export const ProcessStatus: FC<ProcessStatusProps> = (props) => {
  const { status, mode, statusUrl, brokerName } = props;
  const { t } = useTranslation();

  const processList = useMemo(() => {
    const bridgeStatus = getBridgeStatus(status!);
    const depositStatus = getDepositStatus(status!);
    const list: TProcessItem[] = [
      {
        title:
          bridgeStatus === SwapState.Pending
            ? t("transfer.swapDeposit.bridging")
            : t("transfer.swapDeposit.bridge"),
        description: t("transfer.swapDeposit.bridge.description"),
        state: bridgeStatus,
      },
      {
        title:
          depositStatus === SwapState.Pending
            ? t("transfer.swapDeposit.depositing")
            : t("transfer.swapDeposit.deposit"),
        description: t("transfer.swapDeposit.deposit.description", {
          brokerName,
        }),
        state: depositStatus,
      },
    ];

    if (mode === SwapMode.Single) {
      return list.slice(1, 2);
    }

    return list;
  }, [mode, brokerName, status, t]);

  const onOk = () => {
    props.onComplete?.(status === SwapProcessStatus.Done);
  };

  const isFailed =
    status === SwapProcessStatus.DepositFailed ||
    status === SwapProcessStatus.BridgeFialed;

  const disabled =
    status === SwapProcessStatus.Bridging ||
    status === SwapProcessStatus.Depositing;

  const showDashLine = processList.length > 1;

  return (
    <div className="oui-font-semibold">
      <Box intensity={600} p={4} r="lg">
        <Flex
          direction="column"
          itemAlign="start"
          gapY={4}
          className="oui-relative"
        >
          {processList.map((item) => (
            <ProcessItem {...item} key={item.title} />
          ))}
          {showDashLine && (
            <div
              className={cn(
                "oui-absolute oui-start-[10px] oui-top-[20px] oui-bottom-[38px]",
                "oui-w-[1px] oui-h-[34px]",
                "oui-border-e oui-border-dashed oui-border-e-line-16",
              )}
            />
          )}
        </Flex>

        <Box my={3}>
          <Divider intensity={8} />
        </Box>

        <Flex
          gapX={1}
          justify="center"
          itemAlign="center"
          className={cn(
            statusUrl ? "oui-text-primary-light" : "oui-text-base-contrast-20",
            statusUrl ? "oui-cursor-pointer" : "oui-cursor-not-allowed",
          )}
          onClick={() => {
            statusUrl && window.open(statusUrl);
          }}
        >
          <Text size="2xs">{t("transfer.swapDeposit.viewStatus")}</Text>
          <TopRightArrowIcon />
        </Flex>
      </Box>

      <Flex
        direction="column"
        justify="center"
        itemAlign="center"
        mt={8}
        gapY={3}
      >
        {isFailed && (
          <Text size="xs" className="oui-text-danger-light">
            {t("transfer.swapDeposit.depositFailed")}
          </Text>
        )}

        <Button
          className="oui-w-full lg:oui-w-[184px]"
          disabled={disabled}
          onClick={onOk}
        >
          {t("common.ok")}
        </Button>
      </Flex>
    </div>
  );
};

const ProcessItem: FC<TProcessItem> = ({ title, description, state }) => {
  return (
    <Flex itemAlign="start" gapX={2} key={title}>
      <Flex width={20} height={20} justify="center">
        <StatusIndicator state={state} />
      </Flex>
      <Flex direction="column" itemAlign="start">
        <Text
          size="sm"
          className={cn(
            state === "pending" || state === "failed"
              ? "oui-text-base-contrast"
              : "oui-text-base-contrast-54",
          )}
        >
          {title}
        </Text>
        <Text
          size="2xs"
          className={cn(
            state === "pending"
              ? "oui-text-base-contrast-54"
              : "oui-text-base-contrast-20",
          )}
        >
          {description}
        </Text>
      </Flex>
    </Flex>
  );
};

const StatusIndicator: FC<{ state?: SwapState }> = ({ state }) => {
  if (state === SwapState.Pending) {
    return <Spinner size="sm" />;
  }

  if (state === SwapState.Failed) {
    return (
      <CloseCircleFillIcon
        size={20}
        opacity={1}
        className="oui-text-danger-light"
      />
    );
  }

  if (state === SwapState.Success) {
    return (
      <CheckedCircleFillIcon
        size={20}
        opacity={1}
        className="oui-text-primary-light"
      />
    );
  }

  return <Box r="full" width={10} height={10} intensity={200}></Box>;
};

function getBridgeStatus(status: SwapProcessStatus) {
  if (status === SwapProcessStatus.Bridging) {
    return SwapState.Pending;
  }
  if (status === SwapProcessStatus.BridgeFialed) {
    return SwapState.Failed;
  }

  if (
    status === SwapProcessStatus.Done ||
    status === SwapProcessStatus.Depositing
  ) {
    return SwapState.Success;
  }
}

function getDepositStatus(status: SwapProcessStatus) {
  if (status === SwapProcessStatus.Depositing) {
    return SwapState.Pending;
  }
  if (status === SwapProcessStatus.DepositFailed) {
    return SwapState.Failed;
  }

  if (status === SwapProcessStatus.Done) {
    return SwapState.Success;
  }
}

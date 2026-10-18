import { useMemo } from "react";
import { useAccount, useGetEnv, useTrack } from "@paxeer-network/hooks";
import { useMutation } from "@paxeer-network/hooks";
import { TrackerEventName } from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { VAULTS_CONTRACT_ADDRESSES } from "../../../contract";
import { OperationType, RoleType } from "../../../types/vault";
import { getToAccountPayloadType } from "../../../utils/operationPayload";
import { useLatestOperationScript } from "./latest.script";

type OperationScript = {
  type: OperationType;
  vaultId: string;
};

export const useOperationScript = (props: OperationScript) => {
  const { account, state } = useAccount();
  const env = useGetEnv();
  const { track } = useTrack();
  const verifyingContract = useMemo(() => {
    return VAULTS_CONTRACT_ADDRESSES[
      env as keyof typeof VAULTS_CONTRACT_ADDRESSES
    ].vaultPvLedger;
  }, [env]);
  const [postVaultOperation] = useMutation("/v1/sv_operation_request");
  const { refetch: refetchOperationHistory } = useLatestOperationScript({
    type: props.type,
    vaultId: props.vaultId,
  });

  const disabledOperation = useMemo(() => {
    return state?.accountId !== state?.mainAccountId;
  }, [state?.accountId, state?.mainAccountId]);

  const handleOperation = async ({
    amount,
    vaultId,
  }: {
    amount: string;
    vaultId: string;
  }) => {
    if (state.accountId !== state.mainAccountId) {
      toast.error(
        "Please switch to the main account to perform this operation.",
      );
      return;
    }

    if (props.type === OperationType.DEPOSIT) {
      if (new Decimal(amount).lt(10)) {
        toast.error("Deposit amount is less than the minimum 10 USDC.");
        return;
      }
    }

    const payloadType = getToAccountPayloadType(props.type, RoleType.LP);
    const requestParams = {
      payloadType,
      amount: new Decimal(Number(amount)).mul(10 ** 6).toString(),
      vaultId,
      token: "USDC",
      domain: {
        name: "paxeer-network",
        version: "1",
        chainId: state.connectWallet?.chainId as number,
        verifyingContract,
      },
    };

    const { message, signatured } =
      await account.generateDexRequest(requestParams);

    try {
      const res = await postVaultOperation({
        message: {
          ...message,
          chainId: state.connectWallet?.chainId as number,
        },
        signature: signatured,
        userAddress: state.address,
        verifyingContract,
      });

      if (!res.success) {
        // Track failure event
        if (props.type === OperationType.DEPOSIT) {
          track(TrackerEventName.vaultDepositFailed, {
            msg: res.message,
          });
        } else {
          track(TrackerEventName.vaultWithdrawFailed, {
            msg: res.message,
          });
        }
        toast.error(res.message);
        return;
      }

      await refetchOperationHistory();

      // Track success event
      if (props.type === OperationType.DEPOSIT) {
        track(TrackerEventName.vaultDepositSuccess, {
          quantity: amount,
          vaultId,
        });
      } else {
        track(TrackerEventName.vaultWithdrawSuccess, {
          quantity: amount,
          vaultId,
        });
      }

      toast.success(`${props.type} successful`);
    } catch (error: unknown) {
      const errorMessage =
        error instanceof Error ? error.message : String(error);

      // Track error event
      if (props.type === OperationType.DEPOSIT) {
        track(TrackerEventName.vaultDepositFailed, {
          msg: errorMessage,
        });
      } else {
        track(TrackerEventName.vaultWithdrawFailed, {
          msg: errorMessage,
        });
      }
      toast.error(errorMessage);
      console.error(error);
    }
  };

  return {
    handleOperation,
    disabledOperation,
  };
};

import { useCallback, useEffect, useRef } from "react";
import { useAccount, useEventEmitter } from "@paxeer-network/hooks";
import { AccountStatusEnum } from "@paxeer-network/types";
import { WALLET_CONNECT_ABORTED } from "../constants/events";

type ValidationHandler = (status: AccountStatusEnum) => void;

export const useWalletConnectValidation = () => {
  const { account } = useAccount();
  const ee = useEventEmitter();
  const handlerRef = useRef<ValidationHandler | null>(null);

  const clearValidation = useCallback(() => {
    if (!handlerRef.current) {
      return;
    }

    account.off("validate:end", handlerRef.current);
    handlerRef.current = null;
  }, [account]);

  const waitForValidation = useCallback(
    (handler: ValidationHandler) => {
      clearValidation();

      const wrappedHandler: ValidationHandler = (status) => {
        if (handlerRef.current === wrappedHandler) {
          handlerRef.current = null;
        }
        handler(status);
      };

      handlerRef.current = wrappedHandler;
      account.once("validate:end", wrappedHandler);

      return () => {
        if (handlerRef.current !== wrappedHandler) {
          return;
        }
        clearValidation();
      };
    },
    [account, clearValidation],
  );

  useEffect(() => {
    ee.on(WALLET_CONNECT_ABORTED, clearValidation);
    return () => {
      ee.off(WALLET_CONNECT_ABORTED, clearValidation);
      clearValidation();
    };
  }, [clearValidation, ee]);

  return {
    waitForValidation,
    clearValidation,
  } as const;
};

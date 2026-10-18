import { useAccount } from "../useAccount";
import { useMaxWithdrawal } from "./useMaxWithdrawal";

interface ConvertOptions {
  token?: string;
}

export const useConvert = (options: ConvertOptions) => {
  const { token = "" } = options;

  const { account } = useAccount();
  const maxAmount = useMaxWithdrawal(token);

  const convert = async (inputs: {
    amount: number;
    slippage: number;
    receivedAsset?: string;
  }) => {
    return account.assetsManager.convert({
      amount: inputs.amount,
      slippage: inputs.slippage,
      converted_asset: token,
      received_asset: inputs.receivedAsset,
    });
  };

  return { maxAmount, convert };
};

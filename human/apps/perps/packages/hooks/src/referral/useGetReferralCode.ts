import { useQuery } from "../useQuery";

export const useGetReferralCode = (
  accountId?: string
): {
  referral_code?: string;
  error: any;
  isLoading: boolean;
} => {
  const { data, error, isLoading } = useQuery<{ referral_code?: string }>(
    typeof accountId === "undefined" ? null : `/v1/public/referral/check_ref_code?account_id=${accountId}`
  );

  if (typeof accountId === "undefined") {
    return {
      referral_code: undefined,
      error: "The account id is empty or undefined",
      isLoading: false,
    };
  }

  return {
    referral_code: data?.referral_code,
    error,
    isLoading,
  };
};

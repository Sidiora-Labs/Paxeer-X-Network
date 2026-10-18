import { useMutation } from "@paxeer-network/hooks";

export type EditRefereeDescriptionParams = {
  user_address: string;
  description: string | null;
};

export type EditRefereeDescriptionResponse = {
  success?: boolean;
  message?: string;
};

export const useEditRefereeDescription = () => {
  const [doEditRefereeDescription, { isMutating }] = useMutation<
    EditRefereeDescriptionResponse,
    Error
  >("/v1/referral/edit_referee_description");

  const editRefereeDescription = async (
    params: EditRefereeDescriptionParams,
  ) => {
    return doEditRefereeDescription(params);
  };

  return {
    editRefereeDescription,
    isMutating,
  } as const;
};

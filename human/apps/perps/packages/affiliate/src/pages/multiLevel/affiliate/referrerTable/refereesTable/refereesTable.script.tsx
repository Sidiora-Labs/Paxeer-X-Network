import { useCallback, useMemo, useState } from "react";
import {
  TableSort,
  modal,
  usePagination,
  useScreen,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import {
  RefereeDataType,
  useMultiLevelReferees,
} from "../../../../../hooks/useMultiLevelReferees";
import { useReferralContext } from "../../../../../provider";
import { ReferralCodeFormType } from "../../../../../types";
import { ReferralCodeFormDialogId } from "../../referralCodeForm/modal";
import { RefereeDescriptionFormDialogId } from "../refereeDescriptionForm";

type RefereesSortableKey =
  | "referee_rebate_rate"
  | "network_size"
  | "volume"
  | "commission";

export type UseRefereesTableScriptProps = {
  enabled?: boolean;
};

export const useRefereesTableScript = (
  props: UseRefereesTableScriptProps = {},
) => {
  const { enabled = true } = props;
  const { isMobile } = useScreen();
  const refereesPaginationUtils = usePagination();

  const [refereesSort, setRefereesSort] = useState<{
    key: RefereesSortableKey;
    order: "asc" | "desc";
  } | null>(null);

  const {
    isLoading: isRefereesLoading,
    mutate: refereesMutate,
    rows: refereesRows,
    meta: refereesMeta,
  } = useMultiLevelReferees({
    enabled,
    fetchAll: isMobile,
    page: refereesPaginationUtils.page,
    pageSize: refereesPaginationUtils.pageSize,
  });

  const refereesPagination = useMemo(() => {
    return refereesPaginationUtils.parsePagination(refereesMeta);
  }, [refereesMeta, refereesPaginationUtils]);

  const { multiLevelRebateInfo, maxRebateRate, multiLevelRebateInfoMutate } =
    useReferralContext();

  const showActionColumn = useMemo(() => {
    return !new Decimal(multiLevelRebateInfo?.bonus_max_rebate_rate ?? 0).eq(0);
  }, [multiLevelRebateInfo]);

  const baseRebateRate = useMemo(() => {
    return multiLevelRebateInfo?.base_rebate_rate ?? 0;
  }, [multiLevelRebateInfo]);

  const onEditReferee = useCallback(
    (type: ReferralCodeFormType, item: RefereeDataType) => {
      const referrerRebateRate =
        type === ReferralCodeFormType.Reset
          ? multiLevelRebateInfo?.referrer_rebate_rate
          : item.referral_rebate_rate;

      modal.show(ReferralCodeFormDialogId, {
        type,
        referralCode: multiLevelRebateInfo?.referral_code,
        maxRebateRate,
        bonusMaxRebateRate: multiLevelRebateInfo?.bonus_max_rebate_rate,
        baseRebateRate: multiLevelRebateInfo?.base_rebate_rate,
        defaultBonusRefereeRebateRate:
          multiLevelRebateInfo?.default_bonus_referee_rebate_rate,
        referrerRebateRate,
        onSuccess: () => {
          multiLevelRebateInfoMutate();
          refereesMutate();
        },
        accountId: item.account_id,
        multiLevelRebateInfo,
        directInvites: multiLevelRebateInfo?.direct_invites,
        directBonusRebateRate: new Decimal(
          multiLevelRebateInfo?.direct_bonus_rebate_rate ?? 0,
        )
          .mul(100)
          .toNumber(),
      });
    },
    [
      maxRebateRate,
      multiLevelRebateInfo?.base_rebate_rate,
      multiLevelRebateInfo?.bonus_max_rebate_rate,
      multiLevelRebateInfo?.default_bonus_referee_rebate_rate,
      multiLevelRebateInfo?.direct_bonus_rebate_rate,
      multiLevelRebateInfo?.direct_invites,
      multiLevelRebateInfo?.referral_code,
      multiLevelRebateInfo?.referrer_rebate_rate,
      multiLevelRebateInfoMutate,
      refereesMutate,
    ],
  );

  const onEditDescription = useCallback(
    (item: RefereeDataType) => {
      modal.show(RefereeDescriptionFormDialogId, {
        address: item.address,
        description: item.description,
        onSuccess: () => {
          refereesMutate();
        },
      });
    },
    [refereesMutate],
  );

  const refereesData = useMemo(() => {
    const rows = refereesRows ?? [];
    if (!refereesSort) return rows;

    const sortableRows = rows.map((row, index) => ({
      row,
      index,
    }));

    sortableRows.sort((a, b) => {
      const valA = Number(a.row[refereesSort.key] ?? 0);
      const valB = Number(b.row[refereesSort.key] ?? 0);
      if (valA < valB) return refereesSort.order === "asc" ? -1 : 1;
      if (valA > valB) return refereesSort.order === "asc" ? 1 : -1;
      return a.index - b.index;
    });

    return sortableRows.map((item) => item.row);
  }, [refereesRows, refereesSort]);

  const onRefereesSort = (sort?: TableSort) => {
    if (!sort) {
      setRefereesSort(null);
      return;
    }
    const sortKey = sort.sortKey as RefereesSortableKey;
    if (
      sortKey &&
      (sortKey === "referee_rebate_rate" ||
        sortKey === "network_size" ||
        sortKey === "volume" ||
        sortKey === "commission")
    ) {
      setRefereesSort({
        key: sortKey,
        order: sort.sort === "asc" ? "asc" : "desc",
      });
    }
  };

  return {
    refereesData,
    refereesPagination,
    isRefereesLoading,
    onEditReferee,
    onEditDescription,
    onRefereesSort,
    showActionColumn,
    baseRebateRate,
  };
};

export type RefereesTableScriptReturns = ReturnType<
  typeof useRefereesTableScript
>;

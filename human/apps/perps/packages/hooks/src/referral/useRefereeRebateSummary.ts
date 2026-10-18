import { usePrivateQuery } from "../usePrivateQuery";
import { RefferalAPI } from "./api";
import { formatDate } from "./format";

type Params = {
    startDate?: Date,
    endDate?: Date
}

export const useRefereeRebateSummary = (params: Params): {
    data?: RefferalAPI.RefereeRebateSummary[],
    mutate: any,
    isLoading: boolean,
} => {
    const path = "/v1/referral/referee_rebate_summary";

    let url = path;
    if (typeof params.endDate !== 'undefined' && typeof params.startDate !== 'undefined') {

        const sDate = minDate(params.startDate, params.endDate);
        const eDate = maxDate(params.startDate, params.endDate);
        const search = new URLSearchParams([]);
        search.set(`start_date`, formatDate(sDate, ));
        search.set(`end_date`, formatDate(eDate,));
        const queryParams = search.toString() || "";
        url = `${path}?${queryParams}`;
    }
    const {
        data,
        mutate,
        isLoading,
    } = usePrivateQuery<RefferalAPI.RefereeRebateSummary[]>(url, {
        revalidateOnFocus: true
    });

    return {
        data,
        mutate,
        isLoading,
    }
}

function minDate(a: Date, b: Date) {
    return a < b ? a : b;
}

function maxDate(a: Date, b: Date) {
    return a > b ? a : b;
}
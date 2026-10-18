import { useMemo } from "react";
import { useDistribution } from "./useDistribution";
import { useRefereeHistory } from "./useRefereeHistory";

export const useCommission = (options?: { size?: number }) => {

    const [referee, { refresh: refreshReferee, loadMore: loadMoreReferee, }] = useRefereeHistory({ size: options?.size });

    const [distribution, { refresh: refreshDistribution, loadMore: loadMoreDistribution, isLoading }] = useDistribution({ size: options?.size });

    const commissionData = useMemo(() => {

        return distribution?.map((item: any) => {

            if ("updated_time" in item) {
                const updateTime = new Date(item.updated_time);

                const index = referee?.findIndex((e: any) => {
                    if ("date" in e) {
                        return compareDate(updateTime, new Date(e.date));
                    }
                    return false;
                });
                if (index !== -1) {
                    return { ...item, volume: referee?.[index].volume };
                }
            }
            return { ...item };

        });

    }, [referee, distribution]);

    const loadMore = () => {
        loadMoreReferee();
        loadMoreDistribution();
    }

    const refresh = () => {
        refreshReferee();
        refreshDistribution();
    }

    return [commissionData, {
        loadMore,
        refresh,
        isLoading
    }];
}

//** compare two date, yyyy-mm-dd */
function compareDate(d1?: Date, d2?: Date) {
    const isEqual = d1 && d2 &&
        d1.getDay() === d2.getDay()
        && d1.getMonth() === d2.getMonth()
        && d1.getFullYear() === d2.getFullYear();


    return isEqual;
}
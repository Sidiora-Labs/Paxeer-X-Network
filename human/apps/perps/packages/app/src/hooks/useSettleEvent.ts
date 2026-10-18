import { useSettleSubscription } from "@paxeer-network/hooks";
import { toast } from "@paxeer-network/ui";
import { useTranslation } from "@paxeer-network/i18n";

export function useSettleEvent() {
  const { t } = useTranslation();

  useSettleSubscription({
    onMessage: (data: any) => {
      const { status } = data;

      // console.log("settle ws: ", data);

      switch (status) {
        case "COMPLETED":
          toast.success(t("settle.settlement.completed"));
          break;
        case "FAILED":
          toast.error(t("settle.settlement.failed"));
          break;
        default:
          break;
      }
    },
  });
}

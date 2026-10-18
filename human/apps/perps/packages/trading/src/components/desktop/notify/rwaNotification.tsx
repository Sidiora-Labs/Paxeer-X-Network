import { useEffect } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  toast,
  Flex,
  CloseIcon,
  ExclamationFillIcon,
  Text,
  useScreen,
} from "@paxeer-network/ui";

const toastId = "rwaOutsideMarketHoursNotify";

const RwaOutsideMarketHoursNotify = () => {
  const { t } = useTranslation();

  const closeNotify = () => {
    toast.remove(toastId);
  };

  const { isMobile } = useScreen();
  useEffect(() => {
    if (isMobile) {
      setTimeout(() => {
        toast.dismiss(toastId);
      }, 3000);
    }
  }, [isMobile]);

  return (
    <Flex r="lg" className="oui-bg-base-6 oui-relative oui-max-w-[348px]">
      <Flex
        direction="column"
        justify="center"
        itemAlign="center"
        className="oui-bg-warning-darken oui-px-[5px] oui-h-full oui-rounded-tl-[10px] oui-rounded-bl-[10px]"
      >
        <ExclamationFillIcon
          opacity={1}
          className="oui-text-base-6"
          size={18}
        />
      </Flex>
      <Flex p={4} mr={1}>
        <Text size="sm" intensity={80} weight="semibold">
          {t("trading.rwa.outsideMarketHours.notify")}
        </Text>
      </Flex>

      <button
        className="oui-w-6 oui-h-6 oui-end-0 oui-top-0 oui-absolute oui-bg-base-8 oui-rounded-tr-[10px] oui-rounded-bl-[10px] oui-inline-flex oui-justify-center oui-items-center oui-gap-2.5 oui-cursor-pointer"
        onClick={closeNotify}
      >
        <CloseIcon opacity={1} className="oui-text-base-contrast" size={16} />
      </button>
    </Flex>
  );
};

const showRwaOutsideMarketHoursNotify = () => {
  toast.custom(<RwaOutsideMarketHoursNotify />, {
    id: toastId,
    duration: 3000,
  });
};

RwaOutsideMarketHoursNotify.displayName = "RwaOutsideMarketHoursNotify";
export { RwaOutsideMarketHoursNotify, showRwaOutsideMarketHoursNotify };

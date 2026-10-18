import { useTranslation } from "@paxeer-network/i18n";
import { Box, Flex } from "@paxeer-network/ui";

export const Countdown = ({
  timeInterval,
}: {
  timeInterval: number | undefined;
}) => {
  const { t } = useTranslation();

  if (!timeInterval) {
    return null;
  }

  // if timeInterval is greater than 30 minutes, return null
  if (timeInterval >= 1800) {
    return null;
  }

  const [days, hours, minutes, seconds] = breakDownSeconds(timeInterval);

  const daysStr = days.toString().padStart(2, "0");
  const hoursStr = hours.toString().padStart(2, "0");
  const minutesStr = minutes.toString().padStart(2, "0");
  const secondsStr = seconds.toString().padStart(2, "0");

  const renderStr = () => {
    if (days > 0) {
      return {
        s1: daysStr,
        s2: hoursStr,
        c1: t("common.dayShort"),
        c2: t("common.hourShort"),
      };
    }

    if (hours > 0) {
      return {
        s1: hoursStr,
        s2: minutesStr,
        c1: t("common.hourShort"),
        c2: t("common.minuteShort"),
      };
    }

    return {
      s1: minutesStr,
      s2: secondsStr,
      c1: t("common.minuteShort"),
      c2: t("common.secondShort"),
    };
  };

  const { s1, s2, c1, c2 } = renderStr();

  return (
    <Flex className="oui-text-base-contrast-54 oui-text-xs oui-font-normal">
      <Box
        ml={2}
        className="oui-bg-base-7 oui-px-1 oui-rounded-md oui-text-base-contrast oui-min-w-[22px] oui-text-center oui-text-xs"
      >
        {s1}
      </Box>
      {c1}
      {" : "}
      <Box
        ml={1}
        className="oui-bg-base-7 oui-px-1 oui-rounded-md oui-text-base-contrast oui-min-w-[22px] oui-text-center oui-text-xs"
      >
        {s2}
      </Box>
      {c2}
    </Flex>
  );
};

function breakDownSeconds(total: number): [number, number, number, number] {
  const SEC_PER_DAY = 86_400;
  const SEC_PER_HOUR = 3_600;
  const SEC_PER_MIN = 60;

  const days = Math.floor(total / SEC_PER_DAY);
  const remAfterDays = total % SEC_PER_DAY;

  const hours = Math.floor(remAfterDays / SEC_PER_HOUR);
  const remAfterHours = remAfterDays % SEC_PER_HOUR;

  const mins = Math.floor(remAfterHours / SEC_PER_MIN);
  const secs = remAfterHours % SEC_PER_MIN;

  return [days, hours, mins, secs];
}

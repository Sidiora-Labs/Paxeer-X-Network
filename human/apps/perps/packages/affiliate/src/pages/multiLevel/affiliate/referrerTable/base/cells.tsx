import { FC, ReactNode, useCallback, MouseEvent } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Tooltip,
  formatAddress,
  Text,
  Flex,
  Grid,
  cn,
  useScreen,
  modal,
} from "@paxeer-network/ui";
import { commifyOptional } from "@paxeer-network/utils";

const dashedUnderline =
  "oui-cursor-pointer oui-underline oui-decoration-dashed oui-underline-offset-4 oui-decoration-base-contrast-36 oui-tracking-[0.03em]";

const useMobileTooltipModal = (content: ReactNode, title?: ReactNode) => {
  const { isMobile } = useScreen();

  return useCallback(
    (e: MouseEvent) => {
      if (!isMobile) return;
      e.preventDefault();
      e.stopPropagation();

      modal.dialog({
        title,
        closable: true,
        size: "sm",
        content: (
          <div className="oui-text-sm oui-leading-5 oui-text-base-contrast">
            {content}
          </div>
        ),
      });
    },
    [isMobile, content, title],
  );
};

export const AddressCell: FC<{
  address: string;
  title?: ReactNode;
}> = ({ address, title }) => {
  const onClick = useMobileTooltipModal(address, title);
  const { isMobile } = useScreen();
  return (
    <Tooltip content={address} open={isMobile ? false : undefined}>
      <Text className={dashedUnderline} onClick={onClick}>
        {formatAddress(address)}
      </Text>
    </Tooltip>
  );
};

export const TooltipCell: FC<{
  text: string;
  tooltip: string | ReactNode;
  title?: ReactNode;
}> = ({ text, tooltip, title }) => {
  const onClick = useMobileTooltipModal(tooltip, title);
  const { isMobile } = useScreen();
  return (
    <Tooltip content={tooltip} open={isMobile ? false : undefined}>
      <Text className={dashedUnderline} onClick={onClick}>
        {text}
      </Text>
    </Tooltip>
  );
};

export const BreakdownCell: FC<{
  total: number;
  direct: number;
  indirect: number;
  directBonus?: number;
  prefix?: string;
  fix?: number;
  title?: ReactNode;
}> = ({ total, direct, indirect, directBonus, prefix, fix = 0, title }) => {
  const { t } = useTranslation();
  const format = (val: number) =>
    commifyOptional(val, {
      fix,
      fallback: "0",
      padEnd: true,
      prefix,
    });

  const directLine = directBonus ? (
    <>
      • {t("affiliate.direct")}: {format(direct + directBonus)}
    </>
  ) : (
    <>
      • {t("affiliate.direct")}: {format(direct)}
    </>
  );

  const tooltipContent = (
    <div className="oui-flex oui-flex-col oui-gap-1">
      <div>{directLine}</div>
      <div>
        • {t("affiliate.indirect")}: {format(indirect)}
      </div>
    </div>
  );
  const onClick = useMobileTooltipModal(tooltipContent, title);
  const { isMobile } = useScreen();

  return (
    <Tooltip content={tooltipContent} open={isMobile ? false : undefined}>
      <Text className={dashedUnderline} onClick={onClick}>
        {format(total)}
      </Text>
    </Tooltip>
  );
};

export const MobileCell: FC<{
  label: string;
  children: ReactNode;
  align?: "start" | "end";
  className?: string;
}> = ({ label, children, align = "start", className }) => {
  return (
    <Flex
      direction="column"
      itemAlign={align}
      className={cn(
        "oui-gap-1.5 oui-font-medium oui-tracking-[0.01em]",
        className,
      )}
    >
      <Text intensity={36} size="2xs" className="oui-leading-[15px]">
        {label}
      </Text>
      <div className="oui-text-sm oui-leading-[20px]">{children}</div>
    </Flex>
  );
};

export const MobileCard: FC<{
  children: ReactNode;
  className?: string;
}> = ({ children, className }) => {
  return (
    <Grid
      cols={3}
      py={3}
      r="xl"
      width="100%"
      className={cn("oui-bg-base-9 oui-gap-x-[6px] oui-gap-y-3", className)}
    >
      {children}
    </Grid>
  );
};

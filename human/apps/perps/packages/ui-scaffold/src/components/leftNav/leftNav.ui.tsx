import { FC, useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  AccountStatusEnum,
  type RouterAdapter,
  type RouteOption,
} from "@paxeer-network/types";
import {
  Sheet,
  SheetContent,
  VectorIcon,
  SwapHorizIcon,
  PeopleIcon,
  Text,
  Divider,
} from "@paxeer-network/ui";
import { MainLogo } from "../main/mainLogo";
import { DefaultTradingViewTncLink } from "../scaffold/scaffold.ui";
import { SubAccountWidget } from "../subAccount";
import {
  CommunityDiscord,
  CommunityDune,
  CommunityTG,
  CommunityX,
} from "./communityIcon";
import { LeftNavScriptReturn } from "./leftNav.script";
import { LeftNavItem, LeftNavProps } from "./leftNav.type";

export type LeftNavUIProps = LeftNavProps &
  Partial<LeftNavScriptReturn> & {
    className?: string;
    logo?: {
      src: string;
      alt: string;
    };
    routerAdapter?: RouterAdapter;
    showSubAccount?: boolean;
  };

export const LeftNavUI: FC<LeftNavUIProps> = (props) => {
  return (
    <>
      <div onClick={props.showSheet} className={props?.className}>
        <VectorIcon />
      </div>
      <LeftNavSheet {...props} />
    </>
  );
};

const LeftNavSheet: FC<LeftNavUIProps> = (props) => {
  const { state } = useAccount();
  const { t } = useTranslation();

  const { primaryMenus, secondaryMenus } = useMemo(() => {
    const primary = (props?.menus || []).filter((m) => !m.isSecondary);
    const secondary = (props?.menus || []).filter((m) => m.isSecondary);
    return { primaryMenus: primary, secondaryMenus: secondary };
  }, [props.menus]);

  const showSubAccount = useMemo(
    () => state.status >= AccountStatusEnum.EnableTrading,
    [state.status],
  );

  const onRouteChange = (option: RouteOption) => {
    props.routerAdapter?.onRouteChange?.(option);
    props.hideSheet?.();
  };

  const subAccountTrigger = useMemo(() => {
    const name =
      state.accountId === state.mainAccountId
        ? state.address
        : state.subAccounts?.find((item) => item.id === state.accountId)
            ?.description || "";
    return (
      <div className="oui-flex oui-w-full oui-cursor-pointer oui-items-center oui-gap-2 oui-rounded-xl oui-bg-base-5 oui-p-3">
        <div>
          <PeopleIcon />
        </div>
        <div className="oui-flex oui-flex-col oui-gap-1 oui-font-semibold">
          <Text.formatted
            rule="address"
            className="oui-text-sm oui-text-base-contrast"
          >
            {name}
          </Text.formatted>
          <Text.formatted
            rule="address"
            className="oui-text-2xs oui-text-base-contrast-36"
          >{`ID: ${state.accountId}`}</Text.formatted>
        </div>
        <div className="oui-ms-auto">
          <SwapHorizIcon />
        </div>
      </div>
    );
  }, [state, t]);

  const openExternalLink = (url: string) => {
    window.open(url, "_blank");
  };

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent
        side="left"
        className="oui-w-[276px] oui-bg-base-8"
        closeable
        closeableSize={24}
        closeOpacity={0.54}
      >
        <div className="oui-relative oui-flex oui-h-full oui-flex-col oui-gap-3">
          <div className="oui-mt-[6px] oui-flex oui-h-[44px] oui-items-center [&_img]:!oui-h-[18px]">
            <MainLogo {...props?.logo} />
          </div>
          {props?.leading}
          {showSubAccount && (
            <SubAccountWidget customTrigger={subAccountTrigger} />
          )}
          {(primaryMenus.length > 0 || secondaryMenus.length > 0) && (
            <div className="oui-flex oui-h-[calc(100vh-260px)] oui-flex-col oui-items-start oui-overflow-y-auto">
              {primaryMenus.map((item) => (
                <NavItem
                  item={item}
                  key={`item-${item.name}`}
                  onClick={onRouteChange}
                />
              ))}
              {secondaryMenus.length > 0 && primaryMenus.length > 0 && (
                <Divider className="oui-my-1 oui-w-full" intensity={8} />
              )}
              {secondaryMenus.map((item) => (
                <NavItem
                  item={item}
                  key={`item-${item.name}`}
                  onClick={onRouteChange}
                />
              ))}
            </div>
          )}
          <div className="oui-z-60 oui-absolute oui-bottom-6 oui-flex oui-w-full oui-flex-col oui-gap-4 oui-bg-base-8">
            <div className="oui-flex oui-items-center oui-justify-around">
              {props.telegramUrl && (
                <div
                  onClick={() => openExternalLink(props.telegramUrl as string)}
                >
                  <CommunityTG width={24} height={24} />
                </div>
              )}
              {props.twitterUrl && (
                <div
                  onClick={() => openExternalLink(props.twitterUrl as string)}
                >
                  <CommunityX width={24} height={24} />
                </div>
              )}
              {props.discordUrl && (
                <div
                  onClick={() => openExternalLink(props.discordUrl as string)}
                >
                  <CommunityDiscord width={24} height={24} />
                </div>
              )}
              {props.duneUrl && (
                <div
                  onClick={() => openExternalLink(props.duneUrl as string)}
                  className="oui-mb-1"
                >
                  <CommunityDune />
                </div>
              )}
            </div>
            {props.customFooter ?? (
              <DefaultTradingViewTncLink className="oui-text-center" />
            )}
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
};

type NavItemProps = {
  item: LeftNavItem;
  onClick?: (option: RouteOption) => void;
};

const NavItem: FC<NavItemProps> = ({ item, onClick }) => {
  const {
    href,
    name,
    icon,
    trailing,
    onlyInMainAccount,
    customRender,
    target,
  } = item;
  const { isMainAccount } = useAccount();
  const onItemClick = () => {
    // Check if href is a full URL (external link)
    const isExternalLink =
      href.startsWith("http://") || href.startsWith("https://");

    if (target) {
      window.open(href, target);
    } else if (isExternalLink) {
      window.location.href = href;
    } else {
      onClick?.({ href: href, name: name, scope: "leftNav" });
    }
  };
  if (typeof customRender === "function") {
    return (
      <div
        className="oui-flex oui-w-full oui-items-center oui-p-3"
        onClick={onItemClick}
      >
        {customRender({ name: name, href: href })}
      </div>
    );
  }
  if (onlyInMainAccount && !isMainAccount) {
    return null;
  }
  const isSecondary = !!item.isSecondary;
  const textClassName = isSecondary
    ? "oui-text-xs oui-font-normal oui-text-base-contrast-54"
    : "oui-text-base oui-font-semibold oui-text-base-contrast-80";
  return (
    <div
      className={`oui-flex oui-w-full oui-items-center oui-gap-2 oui-p-3 ${isSecondary ? "oui-py-2" : ""}`}
      onClick={onItemClick}
    >
      <div>{icon}</div>
      <div className={textClassName}>{name}</div>
      {trailing}
    </div>
  );
};

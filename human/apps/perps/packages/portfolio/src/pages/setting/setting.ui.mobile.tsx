import { FC, SVGProps, useCallback, useRef, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Card,
  cn,
  Flex,
  Switch,
  Text,
  ChevronRightIcon,
} from "@paxeer-network/ui";
import { AuthGuardTooltip } from "@paxeer-network/ui-connector";
import { LanguageSwitcherWidget } from "@paxeer-network/ui-scaffold";
import { SoundRadioButton } from "./components/soundRadioButton";
import { ThemeSettingCard } from "./components/themeSettingCard";
import type { SettingScriptReturns } from "./setting.script";

type OrderPanelPosition = "left" | "right";

export const SettingMobile: FC<SettingScriptReturns> = (props) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState<boolean>(false);
  const onLanguageChange = () => {
    setOpen(true);
  };

  const audioRef = useRef<HTMLAudioElement | null>(null);
  const playPreview = useCallback((media?: string) => {
    if (!media) return;
    try {
      if (!audioRef.current) {
        audioRef.current = new Audio();
      }
      const audio = audioRef.current;
      audio.pause();
      audio.currentTime = 0;
      audio.src = media;
      void audio.play().catch(() => undefined);
    } catch {
      // ignore runtime audio errors
    }
  }, []);

  const renderItem = (position: OrderPanelPosition) => {
    const isSelected = props.orderPanelLayout === position;
    const handleClick = () => props.setOrderPanelLayout(position);
    const handleKeyDown = (e: React.KeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        handleClick();
      }
    };
    return (
      <Flex
        key={position}
        direction="column"
        gapY={2}
        className="oui-group oui-flex-1 oui-cursor-pointer"
        onClick={handleClick}
        onKeyDown={handleKeyDown}
        role="button"
        tabIndex={0}
        aria-pressed={isSelected}
        aria-label={`Layout ${position}`}
      >
        <Flex
          direction="column"
          className={cn(
            "oui-h-[160px] oui-w-[120px] oui-shrink-0 oui-overflow-hidden oui-rounded-[10px] oui-border-4 oui-border-base-5 oui-bg-base-10 oui-p-0.5",
            "group-hover:oui-border-base-4",
            isSelected && "!oui-border-primary-light",
          )}
        >
          <Flex
            direction="row"
            itemAlign="stretch"
            className="oui-h-[152px] oui-shrink-0 oui-gap-0.5"
          >
            {position === "left" ? (
              <>
                <OrderEntryStripIcon />
                <EmptyBlockIcon />
              </>
            ) : (
              <>
                <EmptyBlockIcon />
                <OrderEntryStripIcon />
              </>
            )}
          </Flex>
          <div className="oui-min-h-[4px] oui-flex-1 oui-bg-base-10" />
        </Flex>
        <Text
          size="2xs"
          intensity={54}
          className={cn(
            "oui-text-center oui-text-base-contrast-54 group-hover:oui-text-base-contrast-80",
            isSelected && "oui-text-base-contrast-80",
          )}
        >
          {position === "left"
            ? t("trading.layout.advanced.left")
            : t("trading.layout.advanced.right")}
        </Text>
      </Flex>
    );
  };

  return (
    <>
      <Flex mt={1} p={4} intensity={900} r="xl" itemAlign="center">
        <LanguageSwitcherWidget open={open} onOpenChange={setOpen} />
        <Flex
          className="oui-cursor-pointer"
          itemAlign="center"
          width="100%"
          onClick={onLanguageChange}
        >
          <Text
            size="base"
            weight="semibold"
            intensity={80}
            className="oui-ms-2"
          >
            {t("languageSwitcher.language")}
          </Text>
          <ChevronRightIcon
            size={18}
            className="oui-ms-auto oui-text-base-contrast-36"
          />
        </Flex>
      </Flex>

      <Card
        // @ts-ignore
        title={
          <div className="oui-text-sm">
            {t("portfolio.setting.systemUpgrade")}
          </div>
        }
        id="portfolio-apikey-manager"
        className="oui-mt-2 oui-bg-base-9 oui-font-semibold"
        classNames={{ root: "oui-p-4", content: "!oui-pt-3" }}
      >
        <Flex
          direction={"row"}
          gap={4}
          width={"100%"}
          itemAlign={"center"}
          pt={3}
          className="oui-border-t-2 oui-border-line-6 oui-font-semibold"
        >
          <Flex
            direction={"column"}
            itemAlign={"start"}
            className="oui-flex-1 oui-gap-2"
          >
            <Text intensity={80} size="xs">
              {t("portfolio.setting.cancelOpenOrders")}
            </Text>
            <Text intensity={36} size="2xs" className="oui-font-normal">
              {t("portfolio.setting.cancelOpenOrders.description")}
            </Text>
            <AuthGuardTooltip>
              <Switch
                className="oui-mt-1"
                checked={props.maintenance_cancel_orders}
                onCheckedChange={(e) => {
                  props.setMaintainConfig(e);
                }}
                disabled={props.isSetting || !props.canTouch}
                data-testid="oui-testid-setting-switch-btn"
              />
            </AuthGuardTooltip>
          </Flex>
        </Flex>
      </Card>

      {(props.hasOrderFilledMedia || props.hasSoundOptions) && (
        <Card
          // @ts-ignore
          title={
            <div className="oui-text-sm">
              {t("portfolio.setting.soundAlerts")}
            </div>
          }
          id="portfolio-sound-alert-setting"
          className="oui-mt-2 oui-bg-base-9 oui-font-semibold"
          classNames={{ root: "oui-p-4", content: "!oui-pt-3" }}
        >
          <Flex
            direction={"row"}
            gap={4}
            width={"100%"}
            itemAlign={"center"}
            pt={3}
            className="oui-border-t-2 oui-border-line-6 oui-font-semibold"
          >
            <Flex
              direction={"column"}
              itemAlign={"start"}
              className="oui-flex-1 oui-gap-2"
            >
              <Text intensity={36} size="2xs" className="oui-font-normal">
                {t("portfolio.setting.soundAlerts.description")}
              </Text>
              <AuthGuardTooltip>
                <Switch
                  className="oui-mt-1"
                  checked={props.soundAlert}
                  onCheckedChange={(e) => {
                    props.setSoundAlert(e);
                  }}
                  disabled={!props.canTouch}
                  data-testid="oui-testid-setting-sound-switch-btn"
                />
              </AuthGuardTooltip>

              {props.hasSoundOptions &&
                props.soundOptions?.length &&
                props.soundAlert && (
                  <Flex direction="row" gap={3} wrap="wrap">
                    {props.soundOptions.map((option) => {
                      const sel = props.selectedSoundValue === option.value;
                      return (
                        <SoundRadioButton
                          key={option.value}
                          sel={sel}
                          label={option.label}
                          onCheckChange={() => {
                            props.setSelectedSoundValue(option.value);
                            props.setSoundAlert(Boolean(option.media));
                            playPreview(option.media);
                          }}
                        />
                      );
                    })}
                  </Flex>
                )}
            </Flex>
          </Flex>
        </Card>
      )}

      <ThemeSettingCard
        themes={props.themes}
        currentThemeId={props.currentThemeId}
        setCurrentThemeId={props.setCurrentThemeId}
        classNames={{
          card: "oui-mt-2 oui-bg-base-9 oui-font-semibold oui-p-3",
          content: "oui-pt-3",
        }}
      />

      <Card
        // @ts-ignore
        title={<div className="oui-text-sm">{t("trading.layout")}</div>}
        id="portfolio-order-panel-setting"
        className="oui-mt-2 oui-bg-base-9 oui-font-semibold"
        classNames={{ root: "oui-p-4", content: "!oui-pt-3" }}
      >
        <Flex
          direction="row"
          gap={4}
          width="100%"
          itemAlign="center"
          pt={3}
          className="oui-border-t-2 oui-border-line-6 oui-font-semibold"
        >
          {renderItem("left")}
          {renderItem("right")}
        </Flex>
      </Card>
    </>
  );
};

const EmptyBlockIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="50"
    height="80"
    viewBox="0 0 50 80"
    fill="currentColor"
    className="oui-fill-base-7"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <rect width="50" height="80" rx="2" />
  </svg>
);

const OrderEntryStripIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="50"
    height="80"
    viewBox="0 0 50 80"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <rect width="50" height="80" rx="2" fill="rgb(var(--oui-color-base-7))" />
    <rect
      x="3"
      y="3"
      width="21"
      height="11"
      rx="2"
      fill="rgb(var(--oui-color-success-darken))"
    />
    <rect
      x="26"
      y="3"
      width="21"
      height="11"
      rx="2"
      fill="rgb(var(--oui-color-danger-darken))"
    />
    <rect
      x="3"
      y="69"
      width="44"
      height="8"
      rx="2"
      fill="rgb(var(--oui-color-success-darken))"
    />
  </svg>
);

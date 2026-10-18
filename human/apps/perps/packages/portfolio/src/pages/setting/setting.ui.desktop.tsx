import { FC, useCallback, useRef } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Card, Flex, Switch, Text } from "@paxeer-network/ui";
import { AuthGuardTooltip } from "@paxeer-network/ui-connector";
import { SoundRadioButton } from "./components/soundRadioButton";
import { ThemeSettingCard } from "./components/themeSettingCard";
import type { SettingScriptReturns } from "./setting.script";

export const SettingDesktop: FC<SettingScriptReturns> = (props) => {
  const { t } = useTranslation();

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
      // ignore play promise rejection (e.g. autoplay policy)
      void audio.play().catch(() => undefined);
    } catch {
      // ignore runtime audio errors
    }
  }, []);

  return (
    <>
      <Card
        title={t("portfolio.setting.systemUpgrade")}
        id="portfolio-apikey-manager"
        className="oui-bg-base-9 oui-font-semibold"
      >
        <Flex
          direction={"row"}
          gap={4}
          width={"100%"}
          itemAlign={"center"}
          pt={4}
          className="oui-border-t-2 oui-border-line-6 oui-font-semibold"
        >
          <Flex direction={"column"} itemAlign={"start"} className="oui-flex-1">
            <Text intensity={80} size="base">
              {t("portfolio.setting.cancelOpenOrders")}
            </Text>
            <Text intensity={54} size="sm">
              {t("portfolio.setting.cancelOpenOrders.description")}
            </Text>
          </Flex>

          <AuthGuardTooltip align="end">
            <Switch
              checked={props.maintenance_cancel_orders}
              onCheckedChange={(e) => {
                props.setMaintainConfig(e);
              }}
              disabled={props.isSetting || !props.canTouch}
              data-testid="oui-testid-setting-switch-btn"
            />
          </AuthGuardTooltip>
        </Flex>
      </Card>

      {(props.hasOrderFilledMedia || props.hasSoundOptions) && (
        <Card
          title={t("portfolio.setting.soundAlerts")}
          id="portfolio-sound-alert-setting"
          className="oui-bg-base-9 oui-font-semibold oui-mt-3"
        >
          <Flex
            direction={"column"}
            gap={3}
            width={"100%"}
            pt={4}
            itemAlign={"start"}
            className="oui-border-t-2 oui-border-line-6 oui-font-semibold"
          >
            <Flex direction={"row"} gap={4} width={"100%"} itemAlign={"center"}>
              <Flex
                direction={"column"}
                itemAlign={"start"}
                className="oui-flex-1"
              >
                <Text intensity={54} size="sm">
                  {t("portfolio.setting.soundAlerts.description")}
                </Text>
              </Flex>

              <AuthGuardTooltip align="end">
                <Switch
                  checked={props.soundAlert}
                  onCheckedChange={(checked) => props.setSoundAlert(checked)}
                  disabled={!props.canTouch}
                  data-testid="oui-testid-setting-sound-switch-btn"
                />
              </AuthGuardTooltip>
            </Flex>

            {props.hasSoundOptions &&
              props.soundOptions?.length &&
              props.soundAlert && (
                <AuthGuardTooltip align="end">
                  <Flex direction="row" gap={3} className="oui-cursor-pointer">
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
                </AuthGuardTooltip>
              )}
          </Flex>
        </Card>
      )}
      <ThemeSettingCard
        themes={props.themes}
        currentThemeId={props.currentThemeId}
        setCurrentThemeId={props.setCurrentThemeId}
        classNames={{
          card: "oui-bg-base-9 oui-font-semibold oui-mt-3",
          content: "oui-pt-4",
        }}
      />
    </>
  );
};

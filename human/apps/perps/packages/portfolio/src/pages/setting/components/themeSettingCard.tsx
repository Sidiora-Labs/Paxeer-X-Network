import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Card, Flex, Select, Button, cn } from "@paxeer-network/ui";
import type { SettingScriptReturns } from "../setting.script";

type ThemeSettingCardProps = Pick<
  SettingScriptReturns,
  "themes" | "currentThemeId" | "setCurrentThemeId"
> & {
  classNames?: {
    card?: string;
    content?: string;
  };
};

export const ThemeSettingCard: FC<ThemeSettingCardProps> = (props) => {
  const { t } = useTranslation();

  const { themes, currentThemeId, setCurrentThemeId, classNames } = props;

  if (!themes || themes.length <= 1) {
    return null;
  }

  return (
    <Card
      title={t("portfolio.setting.theme")}
      id="portfolio-theme-setting"
      classNames={{ root: classNames?.card }}
    >
      <Flex
        direction={"row"}
        gap={4}
        width={"100%"}
        itemAlign={"center"}
        className={cn(
          "oui-border-t-2 oui-border-line-6 oui-font-semibold",
          classNames?.content,
        )}
      >
        {themes.length === 2 ? (
          <Flex direction="row" itemAlign="center" gap={2}>
            {themes.map((theme) => (
              <Button
                variant={theme.id === currentThemeId ? "contained" : "outlined"}
                size="sm"
                key={theme.id}
                onClick={() => setCurrentThemeId?.(theme.id)}
              >
                {theme.displayName}
              </Button>
            ))}
          </Flex>
        ) : (
          <Flex>
            <Select.options
              value={currentThemeId}
              onValueChange={(value) => {
                setCurrentThemeId?.(value);
              }}
              options={themes.map((theme) => ({
                value: theme.id,
                label: theme.displayName,
              }))}
              size="sm"
              testid="oui-testid-setting-theme-select"
              classNames={{ trigger: "oui-min-w-[100px]" }}
            />
          </Flex>
        )}
      </Flex>
    </Card>
  );
};

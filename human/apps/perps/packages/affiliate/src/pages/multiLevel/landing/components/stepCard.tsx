import { FC } from "react";
import { Flex, Text, cn, usePaxeerNetworkTheme } from "@paxeer-network/ui";

/**
 * Icon type for step card
 */
export type IconType = "wallet" | "settings" | "rocket";

/**
 * StepCard component props
 */
export type StepCardProps = {
  step: number;
  icon: IconType;
  title: string;
  description: string;
  className?: string;
};

/**
 * StepCard component - displays a step card with icon, title and description
 */
export const StepCard: FC<StepCardProps> = (props) => {
  const { step, icon, title, description, className } = props;
  const { currentTheme } = usePaxeerNetworkTheme();
  const isLight = currentTheme?.mode === "light";

  // Use one shared brand surface for every card in the light theme.
  const cardBackgroundClass = isLight
    ? "oui-bg-primary"
    : "oui-bg-primary-darken";
  const badgeDigitClass = isLight
    ? "oui-text-base-foreground"
    : "oui-text-primary-contrast";

  return (
    <Flex
      direction="column"
      r="2xl"
      itemAlign={"start"}
      className={cn(
        "oui-stepCard",
        "oui-relative oui-gap-0 oui-text-primary-contrast",
        "lg:oui-min-h-[320px] lg:oui-py-12 lg:oui-px-8",
        cardBackgroundClass,
        className,
      )}
    >
      {/* Icon */}
      <Flex
        justify="center"
        className={cn(
          "oui-stepCard-icon",
          "oui-h-12 oui-w-12 oui-flex-none oui-p-0 lg:oui-justify-start",
        )}
        itemAlign="center"
      >
        {renderIcon(icon)}
      </Flex>

      {/* Title with inline step badge */}
      <div
        className={cn(
          "oui-stepCard-title",
          "oui-relative oui-isolate oui-mt-8 oui-w-full oui-min-h-[2.4em] oui-translate-y-3 oui-pl-7",
          "oui-text-xl oui-font-semibold oui-leading-[1.2] oui-tracking-[-0.02em]",
        )}
      >
        <span
          className={cn(
            "oui-absolute oui-left-0 oui-top-0 oui-z-[3]",
            "oui-inline-flex oui-h-6 oui-w-6 oui-items-center oui-justify-center",
            "oui-rounded-full oui-bg-base-10",
            "oui-text-xs oui-font-semibold oui-leading-none",
            badgeDigitClass,
          )}
        >
          {step}
        </span>
        {title}
      </div>

      {/* Description */}
      <Text
        className={cn(
          "oui-stepCard-description",
          "oui-mt-2.5 oui-max-w-[34ch] oui-text-sm oui-leading-[1.45]",
        )}
      >
        {description}
      </Text>
    </Flex>
  );
};

/**
 * Icon rendering function - renders the appropriate icon based on type
 */
const renderIcon = (iconType: IconType) => {
  switch (iconType) {
    case "wallet":
      return <WalletIcon />;
    case "settings":
      return <SettingsIcon />;
    case "rocket":
      return <RocketIcon />;
    default:
      return null;
  }
};

// SVG Icons
const WalletIcon = () => (
  <svg
    width="24"
    height="24"
    viewBox="0 0 24 24"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className="oui-size-12 oui-fill-primary-contrast"
  >
    <path d="M7.006 2.93a4 4 0 0 0-4 4v10a4 4 0 0 0 4 4h10c1.552 0 3-1.448 3-3l-.004-1.163a3.025 3.025 0 0 0 2.004-2.837v-4c0-1.268-.822-2.428-1.994-2.815l-.006-1.185c0-1.553-1.448-3-3-3zm0 2h10c.448 0 1 .552 1 1v1h-3a3 3 0 0 0-3 3v4a3 3 0 0 0 3 3h3v1c0 .447-.552 1-1 1h-10a2 2 0 0 1-2-2v-10a2 2 0 0 1 2-2m8 4h4a1 1 0 0 1 1 1v4a1 1 0 0 1-1 1h-4a1 1 0 0 1-1-1v-4a1 1 0 0 1 1-1m2 2a1 1 0 1 0 0 2 1 1 0 0 0 0-2" />
  </svg>
);

const SettingsIcon = () => (
  <svg
    width="24"
    height="24"
    viewBox="0 0 24 24"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className="oui-size-12 oui-fill-primary-contrast"
  >
    <path d="M14.99 2.93c-1.267 0-2.45.832-2.818 2.002L3.99 4.93a1 1 0 0 0 0 2l8.18-.001c.449 1.225 1.552 2 2.82 2s2.382-.777 2.838-2.008l2.162.009a1 1 0 0 0 0-2h-2.17c-.482-1.22-1.562-2-2.83-2m0 2a1 1 0 1 1 0 2 1 1 0 0 1 0-2m-6 4c-1.317 0-2.42.82-2.823 2.002-.14.009-2.176-.002-2.176-.002a1 1 0 0 0 0 2s2.052-.021 2.18-.008a2.95 2.95 0 0 0 2.82 2.008c1.268 0 2.354-.777 2.83-1.997l8.17-.003a1 1 0 0 0 0-2l-8.176-.01A3.01 3.01 0 0 0 8.99 8.93m0 2a1 1 0 1 1 0 2 1 1 0 0 1 0-2m6 4c-1.267 0-2.427.835-2.819 1.99l-8.18.01a1 1 0 0 0 0 2h8.165a3.02 3.02 0 0 0 2.835 2c1.268 0 2.36-.793 2.842-2h2.158a1 1 0 0 0 0-2l-2.167-.006a3.03 3.03 0 0 0-2.833-1.994m0 2a1 1 0 1 1 0 2 1 1 0 0 1 0-2" />
  </svg>
);

const RocketIcon = () => (
  <svg
    width="24"
    height="24"
    viewBox="0 0 24 24"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className="oui-size-12 oui-fill-primary-contrast"
  >
    <path d="M18.527 13.88c-.423.424-.884.808-1.345 1.154a.63.63 0 0 0-.269.423 9.2 9.2 0 0 1-2.459 4.346 10 10 0 0 1-1.883 1.462c-.576.346-1.268-.231-1.037-.885.384-1 .538-2.076.538-3.192-.077 0-.192.039-.27.039a39 39 0 0 1-2.612-2.423 51 51 0 0 1-2.42-2.615c0-.077.038-.193.038-.27-1.115 0-2.19.193-3.19.539-.614.23-1.23-.462-.883-1.039a10 10 0 0 1 1.46-1.884 9.2 9.2 0 0 1 4.342-2.461c.153-.039.307-.154.422-.27.346-.5.73-.923 1.153-1.346 2.997-3 7.185-4.038 11.066-3.153a.76.76 0 0 1 .538.538c.845 3.846-.192 8.038-3.189 11.038m-1.69-6.73a2.41 2.41 0 0 0-3.382 0 2.416 2.416 0 0 0 0 3.385 2.41 2.41 0 0 0 3.381 0c.96-.962.96-2.462 0-3.385M8.266 18.92c-.73.73-5.225 3.807-6.109 2.923-.884-.885 2.19-5.385 2.92-6.115s2.037-.616 2.92.269c.884.885 1 2.192.27 2.923" />
  </svg>
);

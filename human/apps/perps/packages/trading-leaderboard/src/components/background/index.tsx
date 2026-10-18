import { FC, useMemo } from "react";
import { cn, useScreen } from "@paxeer-network/ui";

type BackgroundProps = {
  backgroundSrc?: string;
};

export const LeaderboardBackground: FC<BackgroundProps> = (props) => {
  const { isMobile } = useScreen();

  const isVideo = useMemo(() => {
    return isVideoSrc(props.backgroundSrc);
  }, [props.backgroundSrc]);

  if (isMobile) {
    return null;
  }

  const linearGradient =
    "linear-gradient(180deg, rgba(var(--oui-color-base-10) / 0.3) 0%, rgba(var(--oui-color-base-10) / 0) 70%, rgba(var(--oui-color-base-10) / 1) 100%)";

  if (isVideo) {
    return (
      <div
        className={cn("oui-absolute oui-start-0 oui-top-0", "oui-size-full")}
      >
        <div
          style={{
            backgroundImage: linearGradient,
            backgroundSize: "cover",
            backgroundRepeat: "no-repeat",
          }}
          className={cn("oui-absolute oui-start-0 oui-top-0", "oui-size-full")}
        />
        <video
          autoPlay
          loop
          muted
          className={cn(
            // rest style
            "oui-border-none oui-bg-transparent oui-outline-none",
            "oui-size-full",
            // "oui-absolute oui-top-0 oui-start-0",
            "oui-object-cover",
            "oui-opacity-50",
          )}
        >
          <source src={props.backgroundSrc} type="video/mp4" />
          <source src={props.backgroundSrc} type="video/webm" />
          <source src={props.backgroundSrc} type="video/ogg" />
          <source src={props.backgroundSrc} type="video/avi" />
          Your browser does not support the video tag.
        </video>
      </div>
    );
  }

  if (props.backgroundSrc) {
    return (
      <div
        style={{
          backgroundImage: `${linearGradient}, url(${props.backgroundSrc}) `,
          backgroundSize: "cover",
          backgroundRepeat: "no-repeat",
        }}
        className={cn(
          "oui-general-leaderboard-background",
          "oui-absolute oui-start-0 oui-top-0 oui-z-[-1]",
          "oui-size-full",
          "oui-opacity-50",
        )}
      />
    );
  }
};

function isVideoSrc(src?: string) {
  const extension = src?.split(".").pop();
  return ["mp4", "webm", "avi", "ogg"].includes(extension ?? "");
}

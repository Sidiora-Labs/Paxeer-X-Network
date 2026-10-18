import { FC } from "react";
import { cn } from "@paxeer-network/ui";

export interface TimelinePoint {
  title: string;
  type: "past" | "active" | "future";
  time: string;
}

export interface CampaignsAxisProps {
  points: TimelinePoint[];
}

export const CampaignsAxis: FC<CampaignsAxisProps> = ({ points }) => {
  if (!points || points.length === 0) {
    return null;
  }

  // Find the index of the active point
  const activeIndex = points.findIndex((point) => point.type === "active");

  // For single point, center it without any lines
  if (points.length === 1) {
    return (
      <div className="oui-flex oui-w-full oui-justify-center">
        <div className="oui-flex oui-flex-col oui-items-center">
          <AxisPoint type={points[0].type} />
          <div className="oui-mt-4 oui-flex oui-flex-col oui-items-center oui-text-center">
            <div className="oui-trading-leaderboard-title oui-mb-1 oui-whitespace-nowrap oui-text-sm oui-font-medium oui-text-base-contrast-54">
              {points[0].title}
            </div>
            <div className="oui-whitespace-nowrap oui-text-xs oui-text-base-contrast-36">
              {points[0].time}
            </div>
          </div>
        </div>
      </div>
    );
  }

  // Helper function to determine line background class
  const getLineBackgroundClass = (segmentIndex: number): string => {
    // If there's an active point and this segment is to the left of it, use oui-bg-base-3
    if (activeIndex !== -1 && segmentIndex < activeIndex) {
      return "oui-bg-base-3";
    }
    // Otherwise use regular color
    return "oui-bg-base-8";
  };

  const widthPercentage = points?.length > 3 ? "110%" : "120%";

  return (
    <div className="oui-flex oui-w-full" style={{ width: widthPercentage }}>
      {points.map((point, index) => {
        const isFirst = index === 0;
        const isLast = index === points.length - 1;

        return (
          <div
            key={index}
            className="oui-relative oui-flex oui-flex-1 oui-flex-col oui-items-center"
          >
            {/* Point container with connecting lines */}
            <div className="oui-relative oui-flex oui-w-full oui-items-center">
              {/* Left line - always occupy space, but invisible for first point */}
              <div
                className={cn([
                  "oui-h-[6px] oui-flex-1",
                  !isFirst && getLineBackgroundClass(index - 1),
                ])}
              />

              {/* Point */}
              <div className="oui-z-10 oui-shrink-0">
                <AxisPoint type={point.type} />
              </div>

              {/* Right line - always occupy space, but invisible for last point */}
              <div
                className={cn([
                  "oui-h-[6px] oui-flex-1",
                  !isLast && getLineBackgroundClass(index),
                ])}
              />
            </div>

            {/* Label below point */}
            <div className="oui-mt-4 oui-flex oui-flex-col oui-items-center oui-text-center">
              <div className="oui-trading-leaderboard-title oui-mb-1 oui-whitespace-nowrap oui-text-sm oui-font-medium oui-text-base-contrast-54">
                {point.title}
              </div>
              <div className="oui-whitespace-nowrap oui-text-xs oui-text-base-contrast-36">
                {point.type !== "active" && point.time}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
};

export const CampaignsAxisMobile: FC<CampaignsAxisProps> = ({ points }) => {
  if (!points || points.length === 0) {
    return null;
  }

  // Find the index of the active point
  const activeIndex = points.findIndex((point) => point.type === "active");

  // Helper function to determine line background class
  const getLineBackgroundClass = (segmentIndex: number): string => {
    // If there's an active point and this segment is above it, use oui-bg-base-3
    if (activeIndex !== -1 && segmentIndex < activeIndex) {
      return "oui-bg-base-3";
    }
    // Otherwise use regular color
    return "oui-bg-base-8";
  };

  return (
    <div className="oui-flex oui-w-full oui-flex-col oui-items-center oui-gap-10">
      {points.map((point, index) => {
        const isFirst = index === 0;
        const isLast = index === points.length - 1;

        return (
          <div key={index} className={cn(["oui-relative"])}>
            {/* Main content container */}
            <div className="oui-flex oui-h-10 oui-items-start oui-gap-4">
              {/* Point with connecting line */}
              <div
                className={cn([
                  "oui-relative oui-flex oui-flex-col oui-items-center",
                  isFirst && "oui-translate-y-[10px]",
                ])}
              >
                <AxisPoint type={point.type} />

                {/* Vertical connecting line - only show if not the last point */}
                {!isLast && (
                  <div
                    className={cn([
                      "oui-h-[64px] oui-w-[6px]",
                      getLineBackgroundClass(index),
                    ])}
                  />
                )}
              </div>

              {/* Text content */}
              <div className="oui-flex oui-flex-col oui-justify-start">
                <div className="oui-trading-leaderboard-title oui-mb-1 oui-text-sm oui-font-medium oui-text-base-contrast-54">
                  {point.title}
                </div>
                <div className="oui-min-w-[160px] oui-text-xs oui-text-base-contrast-36">
                  {point.type !== "active" && point.time}
                </div>
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
};

const AxisPoint: FC<{ type: "past" | "active" | "future" }> = ({ type }) => {
  return (
    <div
      className={cn([
        "oui-size-5 oui-shrink-0 oui-rounded-full",
        type === "past" && "oui-bg-base-contrast-80",
        type === "active" &&
          "oui-bg-gradient-to-r oui-from-[rgba(var(--oui-gradient-brand-start))] oui-to-[rgba(var(--oui-gradient-brand-end))]",
        type === "future" &&
          "oui-border-[2.5px] oui-border-solid oui-border-base-contrast-80 oui-bg-base-10",
      ])}
    />
  );
};

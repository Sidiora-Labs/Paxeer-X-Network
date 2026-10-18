import { FC } from "react";
import { CampaignsAxis, TimelinePoint } from "./axis";

export const AxisExample: FC = () => {
  // Sample timeline data
  const timelinePoints: TimelinePoint[] = [
    {
      title: "Battle starts",
      type: "past",
      time: "2023-12-13 12:00 UTC",
    },
    {
      title: "Battle ends",
      type: "active",
      time: "2023-12-13 12:00 UTC",
    },
    {
      title: "Reward distribution",
      type: "future",
      time: "2023-12-13 12:00 UTC",
    },
  ];

  return (
    <div className="oui-p-8">
      <h2 className="oui-text-lg oui-font-semibold oui-mb-6 oui-text-center">
        Campaign Timeline
      </h2>
      <CampaignsAxis points={timelinePoints} />
    </div>
  );
};

// Example with more points
export const ExtendedAxisExample: FC = () => {
  const extendedTimelinePoints: TimelinePoint[] = [
    {
      title: "Registration",
      type: "past",
      time: "2023-12-10 00:00 UTC",
    },
    {
      title: "Battle starts",
      type: "past",
      time: "2023-12-13 12:00 UTC",
    },
    {
      title: "Mid checkpoint",
      type: "active",
      time: "2023-12-15 12:00 UTC",
    },
    {
      title: "Battle ends",
      type: "future",
      time: "2023-12-20 12:00 UTC",
    },
    {
      title: "Reward distribution",
      type: "future",
      time: "2023-12-21 12:00 UTC",
    },
  ];

  return (
    <div className="oui-p-8">
      <h2 className="oui-text-lg oui-font-semibold oui-mb-6 oui-text-center">
        Extended Campaign Timeline
      </h2>
      <CampaignsAxis points={extendedTimelinePoints} />
    </div>
  );
};

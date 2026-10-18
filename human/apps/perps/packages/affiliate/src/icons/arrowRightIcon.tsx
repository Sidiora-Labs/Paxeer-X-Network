import { FC } from "react";
import { cn } from "@paxeer-network/ui";

export const ArrowRightIcon: FC<{
  className?: string;
}> = (props) => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      xmlns="http://www.w3.org/2000/svg"
      className={cn(props.className, "oui-fill-base-contrast")}
    >
      <path d="M4.00806 7.9952C4.00806 7.6272 4.30626 7.3292 4.67406 7.3292C4.99259 7.3292 8.71739 7.3292 9.71072 7.3292L7.73339 5.3312L8.67005 4.39453L11.8127 7.51654C11.9434 7.64654 12.0081 7.8212 12.0081 7.99587C12.0081 8.16987 11.9427 8.34388 11.8127 8.47388L8.67005 11.5959L7.73339 10.6592L9.71072 8.66119C8.71739 8.66119 4.99259 8.66119 4.67406 8.66119C4.30626 8.66119 4.00806 8.3632 4.00806 7.9952Z" />
    </svg>
  );
};

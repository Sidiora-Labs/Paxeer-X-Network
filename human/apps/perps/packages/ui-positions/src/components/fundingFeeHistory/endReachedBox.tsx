import { FC, useRef, type PropsWithChildren } from "react";
import { useEndReached } from "./useEndReached";

export interface EndReachedBoxProps {
  onEndReached?: () => void;
  // className?: string;
}

export const EndReachedBox: FC<PropsWithChildren<EndReachedBoxProps>> = (
  props,
) => {
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const { onEndReached } = props;

  useEndReached(sentinelRef, () => {
    onEndReached?.();
  });

  return (
    <>
      {props.children}
      <div
        ref={sentinelRef}
        className="oui-relative oui-invisible oui-h-[25px] oui-bg-red-400 oui-top-[-300px]"
      />
    </>
  );
};

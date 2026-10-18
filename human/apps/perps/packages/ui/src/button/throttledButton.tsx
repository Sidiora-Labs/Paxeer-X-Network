import React, { PropsWithChildren, useCallback, useRef, useState } from "react";
import { Button, ButtonProps } from ".";

const ThrottledButton = React.forwardRef<
  HTMLButtonElement,
  PropsWithChildren<ButtonProps & { throttleDuration?: number }>
>(({ onClick, throttleDuration = 700, ...props }, ref) => {
  const lastCall = useRef(0);
  const throttle = useCallback(
    (delay: number, fn?: Function) => {
      return function (...args: any[]) {
        const now = Date.now();

        // Check if enough time has passed since the last call
        if (now - lastCall.current >= delay) {
          lastCall.current = now;
          fn?.(...args); // Execute the function
        }
      };
    },
    [throttleDuration]
  );

  const debouncedClick = throttle(throttleDuration, onClick);
  return <Button onClick={debouncedClick} ref={ref} {...props} />;
});

export { ThrottledButton };

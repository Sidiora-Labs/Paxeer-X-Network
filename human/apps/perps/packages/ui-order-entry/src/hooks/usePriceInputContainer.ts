import { useEffect, useRef, useState } from "react";
import { OrderType } from "@paxeer-network/types";

export function usePriceInputContainer({
  order_type_ext,
}: {
  order_type_ext?: OrderType;
}) {
  const [priceInputContainerWidth, setPriceInputContainerWidth] = useState(0);
  const priceInputContainerRef = useRef<HTMLDivElement | null>(null);

  // useEffect(() => {
  //   if (
  //     priceInputContainerRef.current &&
  //     // update BBO select width when is BBO order
  //     isBBOOrder({ order_type_ext: formattedOrder.order_type_ext })
  //   ) {
  //     const width =
  //       priceInputContainerRef.current.getBoundingClientRect()?.width;
  //     if (width) {
  //       setPriceInputContainerWidth(width);
  //     }
  //   }
  // }, [priceInputContainerRef, formattedOrder.order_type_ext]);

  useEffect(() => {
    const element = priceInputContainerRef.current;

    if (!element) {
      return;
    }

    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const width = entry.contentRect.width;
        if (width) {
          // update BBO order select dropdown width when priceInputContainerRef width changed
          setPriceInputContainerWidth(width);
        }
      }
    });

    resizeObserver.observe(element);

    return () => {
      resizeObserver.unobserve(element);
    };
  }, [priceInputContainerRef, order_type_ext]);

  return { priceInputContainerRef, priceInputContainerWidth };
}

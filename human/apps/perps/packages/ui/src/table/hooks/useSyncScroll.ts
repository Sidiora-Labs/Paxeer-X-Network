import { useEffect, useRef, useState } from "react";

export function useSyncScroll(deps: any[]) {
  const [isXScroll, setIsXScroll] = useState(false);
  const [isYScroll, setIsYScroll] = useState(false);
  const theadRef = useRef<HTMLDivElement>(null);
  const tbodyRef = useRef<HTMLDivElement>(null);
  const [headerHeight, setHeaderHeight] = useState(40);

  useEffect(() => {
    if (theadRef.current && tbodyRef.current) {
      tbodyRef.current.addEventListener("scroll", function (e) {
        theadRef.current!.scrollTo({
          left: tbodyRef.current!.scrollLeft,
        });
      });

      theadRef.current.addEventListener("scroll", function (e) {
        tbodyRef.current!.scrollTo({ left: theadRef.current!.scrollLeft });
      });
    }
  }, [theadRef, tbodyRef]);

  useEffect(() => {
    // Whether to scroll depends on the deps
    if (theadRef.current && tbodyRef.current) {
      setIsXScroll(hasHorizontalScroll(tbodyRef.current));
      setIsYScroll(hasVerticalScroll(tbodyRef.current));
      setHeaderHeight(theadRef.current?.clientHeight);
    }
  }, [theadRef, tbodyRef, ...deps]);

  return { theadRef, tbodyRef, isXScroll, isYScroll, headerHeight };
}

function hasHorizontalScroll(element: HTMLDivElement) {
  return element.scrollWidth > element.clientWidth;
}

function hasVerticalScroll(element: HTMLDivElement) {
  return element.scrollHeight > element.clientHeight;
}

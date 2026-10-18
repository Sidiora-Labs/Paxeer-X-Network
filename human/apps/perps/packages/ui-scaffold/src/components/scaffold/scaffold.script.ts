import { useEffect, useRef, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { useObserverElement, useScreen } from "@paxeer-network/ui";
import { type ScaffoldProps } from "./scaffold.widget";

export type ScaffoldScriptReturn = ReturnType<typeof useScaffoldScript>;

type ScaffoldScriptOptions = ScaffoldProps;

export const useScaffoldScript = (options: ScaffoldScriptOptions) => {
  const { restrictedInfo, showAnnouncement } = useAppContext();

  const [topNavbarRef, topNavbarHeight] = useRefAndHeight(48);
  const [footerRef, footerHeight] = useRefAndHeight(29);
  const [bottomNavRef, bottomNavHeight] = useRefAndHeight(64);
  const [announcementRef, announcementHeight] = useRefAndHeight(0, [
    showAnnouncement,
  ]);

  const [expand, setExpand] = useLocalStorage(
    "paxeerNetwork_scaffold_expanded",
    true,
  );

  const { isMobile } = useScreen();

  const sideBarExpandWidth = options.leftSideProps?.maxWidth || 185;
  const sideBarCollaspedWidth = options.leftSideProps?.minWidth || 98;
  const hasLeftSidebar = !!options.leftSidebar;

  return {
    topNavbarRef,
    footerRef,
    topNavbarHeight,
    footerHeight,
    announcementRef,
    announcementHeight,
    restrictedInfo,
    expand,
    setExpand,
    isMobile,
    sideBarExpandWidth,
    sideBarCollaspedWidth,
    hasLeftSidebar,
    footerProps: options.footerProps,
    routerAdapter: options.routerAdapter,
    mainNavProps: options.mainNavProps,
    bottomNavProps: options.bottomNavProps,
    bottomNavRef,
    bottomNavHeight,
  };
};

const useRefAndHeight = (
  defaultHeight: number,
  deps: React.DependencyList = [],
) => {
  const ref = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(defaultHeight);

  useObserverElement(ref.current, (entry) => {
    setHeight(entry.contentRect.height);
  });

  useEffect(() => {
    if (!ref.current) {
      return;
    }
    const rect = ref.current?.getBoundingClientRect();
    setHeight(rect.height);
  }, [ref, ...deps]);

  return [ref, height] as const;
};

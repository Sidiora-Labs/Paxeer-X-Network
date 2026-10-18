import { useMediaQuery } from "./useMediaQuery";

export function useScreen() {
  const isMobile = useMediaQuery("(max-width: 1023.98px)");

  return {
    isMobile,
    isDesktop: !isMobile,
  };
}

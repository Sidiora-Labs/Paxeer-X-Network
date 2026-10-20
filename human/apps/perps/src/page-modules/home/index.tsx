import { useEffect } from "react";
import { useRouter } from "next/router";

/**
 * Home dispatcher — single PRO surface.
 *
 * `/` always redirects to `/perps`. The legacy `/?market=X` form is
 * preserved by passing the query through, so existing bookmarks keep
 * working.
 */
export default function HomePage() {
  const router = useRouter();

  useEffect(() => {
    if (!router.isReady) return;
    const market = router.query.market;
    if (typeof market === "string" && market.length > 0) {
      router.replace(`/perps?market=${market}`);
    } else {
      router.replace("/perps");
    }
  }, [router.isReady, router.query.market, router]);

  return null;
}

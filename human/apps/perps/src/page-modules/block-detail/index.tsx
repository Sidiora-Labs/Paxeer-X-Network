import type { GetServerSideProps } from "next";

/**
 * Legacy `/block/[height]` route — kept per Supervisor DECISION 017 as a
 * ~10 LOC redirect shell to `/explorer?block=<height|hash>`. Accepts either
 * a numeric height or a block hash (both resolve via Paxscan's
 * `height_or_hash` path-param convention inside the explorer panel).
 */
export default function BlockRedirectShell() {
  return null;
}

export const getServerSideProps: GetServerSideProps = async (context) => {
  const raw = context.params?.height;
  const value = Array.isArray(raw) ? raw[0] : raw;
  if (!value) {
    return { redirect: { destination: "/explorer", permanent: false } };
  }
  return {
    redirect: {
      destination: `/explorer?block=${encodeURIComponent(value)}`,
      permanent: false,
    },
  };
};

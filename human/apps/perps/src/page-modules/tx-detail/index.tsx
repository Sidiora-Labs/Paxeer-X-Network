import type { GetServerSideProps } from "next";

/**
 * Legacy `/tx/[hash]` route — kept per Supervisor DECISION 017 as a ~10 LOC
 * redirect shell to `/explorer?tx=<hash>`. Server-side redirect avoids any
 * client flash; Agent 4's deep-links keep working without contract change.
 */
export default function TxRedirectShell() {
  // Server redirect means this component should never render on a real request.
  // Returning `null` keeps bundle weight minimal if the fallback does fire.
  return null;
}

export const getServerSideProps: GetServerSideProps = async (context) => {
  const raw = context.params?.hash;
  const hash = Array.isArray(raw) ? raw[0] : raw;
  if (!hash) {
    return { redirect: { destination: "/explorer", permanent: false } };
  }
  return {
    redirect: {
      destination: `/explorer?tx=${encodeURIComponent(hash)}`,
      permanent: false,
    },
  };
};

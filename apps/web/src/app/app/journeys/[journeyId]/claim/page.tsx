import { custodyTimingFromEnv, JourneyScreen } from "../../../../../journeys/custody";

export const dynamic = "force-dynamic";

export default async function JourneyClaimPage({
  params,
}: Readonly<{ params: Promise<{ journeyId: string }> }>) {
  const { journeyId } = await params;
  return <JourneyScreen journeyId={journeyId} timing={custodyTimingFromEnv(process.env)} />;
}

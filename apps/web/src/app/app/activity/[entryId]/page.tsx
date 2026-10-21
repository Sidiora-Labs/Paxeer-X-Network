import { ActivityDetail } from "../../../../journeys/activity";

export default async function ActivityDetailPage({
  params,
}: Readonly<{ params: Promise<{ entryId: string }> }>) {
  return <ActivityDetail entryId={(await params).entryId} />;
}

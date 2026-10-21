import { custodyTimingFromEnv } from "../../../journeys/custody";
import { Withdraw } from "../../../journeys/withdraw";

export const dynamic = "force-dynamic";

export default function WithdrawPage() {
  return <Withdraw timing={custodyTimingFromEnv(process.env)} />;
}

import { custodyTimingFromEnv } from "../../../journeys/custody";
import { Deposit } from "../../../journeys/deposit";

export const dynamic = "force-dynamic";

export default function DepositPage() {
  return <Deposit timing={custodyTimingFromEnv(process.env)} />;
}

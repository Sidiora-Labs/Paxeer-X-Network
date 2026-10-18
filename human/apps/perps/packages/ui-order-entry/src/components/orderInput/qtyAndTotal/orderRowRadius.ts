import { cn } from "@paxeer-network/ui";

export type OrderInputStackPosition = "top" | "middle" | "bottom";

export function stackedOrderInputRoundedCn(position: OrderInputStackPosition) {
  return cn(
    "!oui-rounded",
    position === "top" && "!oui-rounded-t-xl",
    position === "bottom" && "!oui-rounded-b-xl",
  );
}

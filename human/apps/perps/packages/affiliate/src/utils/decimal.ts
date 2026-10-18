import { commify } from "@paxeer-network/utils";

export function refCommify(value?: string | number, fix?: number) {
  if (value === undefined || value === null) {
    return "-";
  }
  return `$${commify(value, fix)}`;
}

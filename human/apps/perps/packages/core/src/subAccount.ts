import { API } from "@paxeer-network/types";

export type SubAccount = {
  id: string;
  description: string;
  holding: API.Holding[];
};

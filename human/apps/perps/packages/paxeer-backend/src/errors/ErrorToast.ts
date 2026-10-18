import { toast } from "@paxeer-network/ui";
export function showErrorToast(error: unknown) { toast.error(error instanceof Error ? error.message : String(error)); }

// Transitional namespace barrel — to be removed in a follow-up phase.
// Re-exports the paxscan public surface from its layered homes.
export * from "@/types/paxscan";
export * from "@/core/paxscan/api";
export * from "@/hooks/paxscan";
export * from "@/core/paxscan/socket";
export * from "@/core/paxscan/urls";
export { PaxscanProvider } from "@/app/providers/PaxscanProvider";
export { PAXSCAN_API_URL, PAXSCAN_WS_URL, PAXSCAN_BASE_URL } from "@/core/paxscan/config";

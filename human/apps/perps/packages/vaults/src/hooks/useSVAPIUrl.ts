import { useMemo } from "react";
import { useGetEnv } from "@paxeer-network/hooks";
import { VAULTS_API_URLS } from "../api/env";

export function useSVApiUrl() {
  const env = useGetEnv();

  const apiUrl = useMemo(() => {
    return VAULTS_API_URLS[env as keyof typeof VAULTS_API_URLS];
  }, [env]);

  return apiUrl;
}

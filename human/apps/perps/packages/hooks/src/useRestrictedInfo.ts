import { ReactNode, useEffect, useState } from "react";
import { API } from "@paxeer-network/types";
import { useLocalStorage } from "./useLocalStorage";
import { useQuery } from "./useQuery";

export type RestrictedInfoReturns = ReturnType<typeof useRestrictedInfo>;

export interface RestrictedInfoOptions {
  enableDefault?: boolean;
  customRestrictedIps?: string[];
  customRestrictedRegions?: string[];
  customUnblockRegions?: string[];
  content?:
    | ReactNode
    | ((data: { ip: string; brokerName: string }) => ReactNode);
}

const defaultRestrictedIps: string[] = [];
const defaultRestrictedRegions: string[] = [];
/** default can unblock regions */
const defaultUnblockRegions: string[] = [];

export const useRestrictedInfo = (options?: RestrictedInfoOptions) => {
  const {
    enableDefault = false,
    customRestrictedIps = defaultRestrictedIps,
    customRestrictedRegions = defaultRestrictedRegions,
    customUnblockRegions = defaultUnblockRegions,
    content,
  } = options || {};
  const [ip, setIp] = useState<string>("");
  const [allInvalidAreas, setAllInvalidAreas] = useState<string[]>([]);
  const [restrictedOpen, setRestrictedOpen] = useState<boolean>(false);
  const [canUnblock, setCanUnblock] = useState<boolean>(false);

  const [accessRestricted, setAccessRestricted] = useLocalStorage<
    boolean | undefined
  >("paxeerNetwork_access_restricted", undefined);

  const { data: ipInfo } = useQuery<API.IpInfo>("/v1/ip_info", {
    revalidateOnFocus: false,
  });

  const { data: restrictedAreas } = useQuery<API.RestrictedAreas>(
    "/v1/restricted_areas",
    {
      revalidateOnFocus: false,
    },
  );

  useEffect(() => {
    if (!restrictedAreas || !ipInfo) {
      return;
    }

    try {
      const { invalid_web_country, invalid_web_city } = restrictedAreas;

      const invalidCountries = invalid_web_country
        ?.toLowerCase()
        .replace(/\s+/g, "")
        .split(",");

      const invalidCities = invalid_web_city
        ?.toLowerCase()
        .replace(/\s+/g, "")
        .split(",");

      const formattedCustomRegions = customRestrictedRegions?.map((item) =>
        formatRegion(item),
      );

      const combinedInvalidRegions = [
        ...formattedCustomRegions,
        ...(enableDefault ? [...invalidCountries, ...invalidCities] : []),
      ].filter((item) => !!item);

      const allInvalidAreas = [
        enableDefault ? invalid_web_country : "",
        enableDefault ? invalid_web_city : "",
        customRestrictedRegions?.join(", "),
      ].filter((item) => !!item);

      const { city, region, ip } = ipInfo;

      const formattedCity = formatRegion(city);
      const formattedRegion = formatRegion(region);

      const isRestricted =
        (formattedCity && combinedInvalidRegions.includes(formattedCity)) ||
        (formattedRegion && combinedInvalidRegions.includes(formattedRegion)) ||
        (ip && customRestrictedIps.includes(ip));

      let canUnblock = false;

      if (isRestricted) {
        for (const item of customUnblockRegions) {
          if (formatRegion(item) === formatRegion(region)) {
            canUnblock = true;
          }
        }
      }

      const restrictedOpen = canUnblock
        ? isRestricted && accessRestricted !== false
        : isRestricted;

      setIp(ip);
      setAllInvalidAreas(allInvalidAreas);
      setRestrictedOpen(!!restrictedOpen);
      setCanUnblock(canUnblock);
    } catch (error) {
      console.error("useRestrictedInfo error", error);
    }
  }, [
    ipInfo,
    restrictedAreas,
    accessRestricted,
    enableDefault,
    // it will lead to infinite re-render when these values change, so we don't need to watch these
    // customRestrictedIps,
    // customRestrictedRegions,
    // customUnblockRegions,
  ]);

  return {
    ip,
    invalidRegions: allInvalidAreas,
    restrictedOpen,
    content,
    canUnblock,
    accessRestricted,
    setAccessRestricted,
  };
};

function formatRegion(region: string) {
  return region?.replace(/\s+/g, "").toLowerCase();
}

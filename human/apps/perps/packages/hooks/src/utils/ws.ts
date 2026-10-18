import { camelCaseToUnderscoreCase } from "@paxeer-network/utils";

export function object2underscore(obj: any) {
  return Object.keys(obj).reduce<any>((acc, key) => {
    acc[camelCaseToUnderscoreCase(key)] = obj[key];
    return acc;
  }, {});
}

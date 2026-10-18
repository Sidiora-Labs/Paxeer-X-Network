/*
{
      message: "I am the paxeer-network KING.",
      domain: "ordely.network",
      updateTime: "2022-JAN-01 23:23",
      position: {
        symbol: "BTC-PERP",
        currency: "USDC",
        side: "LONG",
        leverage: 20,
        pnl: 10432.23,
        ROI: 20.25,
        informations: [
          { title: "Open Price", value: 0.12313 },
          { title: "Opened at", value: "Jan-01 23:23" },
          { title: "Mark price", value: "0.12341" },
          { title: "Quantity", value: "0.123" },
        ],
      },
      referral: {
        code: "WRECKED",
        link: "https://paxeer-network.network",
        slogan: "Try paxeer-network now with:",
      }
    }
*/
// import { PnLDisplayFormat, ShareOptions } from "./type";
import { Decimal, formatNum } from "@paxeer-network/utils";
import { PnLDisplayFormat, ShareEntity, ShareOptions } from "../../types/types";

export type ReferralType = {
  code?: string;
  link?: string;
  slogan?: string;
};

export function getPnLPosterData(
  position: ShareEntity,
  message: string,
  domain: string,
  pnlType: PnLDisplayFormat,
  options: Set<ShareOptions>,
  t: (key: string) => string,
  baseDp?: number,
  quoteDp?: number,
  referral?: ReferralType,
  brokerName?: string,
) {
  const { symbol, currency } = processSymbol(position.symbol);
  const positionData: any = {
    symbol,
    currency,
    side: position.side,
    marginMode: position.marginMode,
  };

  const normalizedBrokerName = brokerName?.trim();
  if (normalizedBrokerName) {
    positionData["brokerName"] = normalizedBrokerName;
  }

  switch (pnlType) {
    case "pnl": {
      if (position.pnl != null) {
        positionData["pnl"] = formatNum.pnl(position.pnl)?.toFixed(2);
      }
      break;
    }
    case "roi": {
      if (position.roi != null) {
        positionData["ROI"] = formatNum.roi(position.roi)?.toFixed(2);
      }
      break;
    }
    case "roi_pnl": {
      if (position.pnl != null) {
        positionData["pnl"] = formatNum.pnl(position.pnl)?.toFixed(2);
      }
      if (position.roi != null) {
        positionData["ROI"] = formatNum.roi(position.roi)?.toFixed(2);
      }
      break;
    }
  }

  const informations: { title: string; value: any }[] = [];

  if (options.has("leverage")) {
    positionData["leverage"] = position.leverage;
  }
  const array: ShareOptions[] = [
    "openPrice",
    "closePrice",
    "openTime",
    "closeTime",
    "markPrice",
    "quantity",
  ];
  array.forEach((key) => {
    if (options.has(key)) {
      switch (key) {
        case "leverage": {
          break;
        }
        case "openPrice": {
          if (position.openPrice != null) {
            informations.push({
              title: t("share.pnl.optionalInfo.openPrice"),
              value: formatFixed(position.openPrice, quoteDp || 2),
            });
          }
          break;
        }
        case "closePrice": {
          if (position.closePrice != null) {
            informations.push({
              title: t("share.pnl.optionalInfo.closePrice"),
              value: formatFixed(position.closePrice, quoteDp || 2),
            });
          }
          break;
        }
        case "openTime": {
          if (position.openTime != null) {
            informations.push({
              title: t("share.pnl.optionalInfo.openTime"),
              value: formatOpenTime(position.openTime),
            });
          }
          break;
        }
        case "closeTime": {
          if (position.closeTime != null) {
            informations.push({
              title: t("share.pnl.optionalInfo.closeTime"),
              value: formatOpenTime(position.closeTime),
            });
          }
          break;
        }
        case "markPrice": {
          if (position.markPrice != null) {
            informations.push({
              title: t("common.markPrice"),
              value: formatFixed(position.markPrice, quoteDp || 2),
            });
          }
          break;
        }
        case "quantity": {
          if (position.quantity != null) {
            informations.push({
              title: t("common.quantity"),
              value: formatFixed(position.quantity, baseDp || 2),
            });
          }
        }
        default:
          break;
      }
    }
  });

  positionData["informations"] = informations;

  const data: any = {
    position: positionData,
    updateTime: formatShareTime(new Date()),
    domain,
  };
  if (message.length > 0) {
    data["message"] = message;
  }

  if (typeof referral !== "undefined" && referral["code"] !== undefined) {
    data["referral"] = referral;
  }

  return data;
}

interface SymbolResult {
  symbol: string;
  currency: string;
}

function processSymbol(symbol: string): SymbolResult {
  const tokens = symbol.split("_");
  if (tokens.length < 3) {
    return {
      symbol: symbol,
      currency: "USDC",
    };
  }

  const [, base, quote] = tokens;

  return {
    symbol: base,
    currency: quote || "USDC",
  };
}

function formatShareTime(input: number): string;
function formatShareTime(input: Date): string;
function formatShareTime(input: number | Date): string {
  const date = input instanceof Date ? input : new Date(input);
  const options: Intl.DateTimeFormatOptions = {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  };

  const formatter = new Intl.DateTimeFormat("en-US", options);
  const formattedParts = formatter.formatToParts(date);

  // console.log("formattedParts", formattedParts);

  const year = formattedParts.find((part) =>
    part.type === "year" ? part.value : "",
  )?.value;
  const month = formattedParts.find((part) =>
    part.type === "month" ? part.value : "",
  )?.value;
  const day = formattedParts.find((part) =>
    part.type === "day" ? part.value : "",
  )?.value;
  const hour = formattedParts.find((part) =>
    part.type === "hour" ? part.value : "",
  )?.value;
  const minute = formattedParts.find((part) =>
    part.type === "minute" ? part.value : "",
  )?.value;

  return `${year}-${month}-${day} ${hour}:${minute}`;
}

function formatOpenTime(input: number | Date): string {
  const date = input instanceof Date ? input : new Date(input);
  const options: Intl.DateTimeFormatOptions = {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  };

  const formatter = new Intl.DateTimeFormat("en-US", options);
  const formattedParts = formatter.formatToParts(date);

  // console.log("formattedParts", formattedParts);

  const month = formattedParts.find((part) =>
    part.type === "month" ? part.value : "",
  )?.value;
  const day = formattedParts.find((part) =>
    part.type === "day" ? part.value : "",
  )?.value;
  const hour = formattedParts.find((part) =>
    part.type === "hour" ? part.value : "",
  )?.value;
  const minute = formattedParts.find((part) =>
    part.type === "minute" ? part.value : "",
  )?.value;

  return `${month}-${day} ${hour}:${minute}`;
}

function formatFixed(value: number, dp: number) {
  return new Decimal(value).toFixed(dp, Decimal.ROUND_DOWN);
}

export function savePnlInfo(
  format: PnLDisplayFormat,
  options: Set<ShareOptions>,
  bgIndex: number,
  message: string,
) {
  localStorage.setItem(
    "pnl_config_key",
    JSON.stringify({
      bgIndex: bgIndex,
      pnlFormat: format,
      options: Array.from(options),
      message: message,
    }),
  );
}

export function getPnlInfo(): {
  bgIndex: number;
  pnlFormat: PnLDisplayFormat;
  options: ShareOptions[];
  message: "";
} {
  const str = localStorage.getItem("pnl_config_key");

  if (str && str.length > 0) {
    try {
      const json = JSON.parse(str);

      return json;
    } catch (e) {}
  }
  return {
    bgIndex: 0,
    pnlFormat: "roi_pnl",
    options: [
      "openPrice",
      "closePrice",
      "openTime",
      "closeTime",
      "markPrice",
      "quantity",
      "leverage",
    ],
    message: "",
  };
}

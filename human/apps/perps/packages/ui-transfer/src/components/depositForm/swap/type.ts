interface SwapOutcomes {
  token: string;
  symbol: string;
  decimals: number;
  amount: string;
}

interface SwapFeesFrom {
  extra: string;
  woofi: string;
  total: string;
}

interface SwapRouteInfos {
  tokens: string[];
  symbols: string[];
  amounts: string[];
  decimals: number[];
}

interface SwapInfos {
  network: string;
  from_token: string;
  from_amount: string;
  to_token: string;
  min_to_amount: string;
}

interface Swap1inch {
  swap_router: string;
  data: string;
}

export interface SwapQuoteData {
  outcomes: SwapOutcomes;
  fees_from: SwapFeesFrom;
  price: number;
  route_infos: SwapRouteInfos;
  infos: SwapInfos;
  "1inch": Swap1inch;
}

/** https://api.woofi.com/v2/swap API response */
// {
//   "status": "ok",
//   "data": {
//       "outcomes": {
//           "token": "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
//           "symbol": "USDC",
//           "decimals": 6,
//           "amount": "2891010619"
//       },
//       "fees_from": {
//           "extra": "0",
//           "woofi": "0.000150000000000000",
//           "total": "0.000150000000000000"
//       },
//       "price": 2891.010619,
//       "route_infos": {
//           "tokens": [
//               "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
//               "0xaf88d065e77c8cC2239327C5EDb3A432268e5831"
//           ],
//           "symbols": [
//               "ETH",
//               "USDC"
//           ],
//           "amounts": [
//               "1000000000000000000",
//               "2891010619"
//           ],
//           "decimals": [
//               18,
//               6
//           ]
//       },
//       "infos": {
//           "network": "arbitrum",
//           "from_token": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
//           "from_amount": "1000000000000000000",
//           "to_token": "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
//           "min_to_amount": "2862100512"
//       },
//       "1inch": {
//           "swap_router": "0x0000000000000000000000000000000000000000",
//           "data": "0x"
//       }
//   }
// }

export interface SwapQuoteSuccessResponse {
  status: "ok";
  data: SwapQuoteData;
}

// {
//   "status": "fail",
//   "error": {
//       "message": {
//           "1inch": {
//               "message": "1inch API Error (status 400)",
//               "error": {
//                   "error": "Bad Request",
//                   "description": "insufficient liquidity",
//                   "statusCode": 400,
//                   "meta": [
//                       {
//                           "type": "toTokenAmount",
//                           "value": "0"
//                       }
//                   ],
//                   "requestId": "7127b07c-2eed-4461-a175-489b7bd92ede"
//               },
//               "status_code": 400
//           }
//       }
//   }
// }
export interface SwapQuoteErrorResponse {
  status: "fail";
  error: {
    message: {
      [key: string]: {
        message: string;
        error: {
          error: string;
          description: string;
          statusCode: number;
          meta: {
            type: string;
            value: string;
          }[];
        };
      };
    };
  };
}

export type SwapQuoteResponse =
  | SwapQuoteSuccessResponse
  | SwapQuoteErrorResponse;

export type SwapNetwork =
  | "bsc"
  | "avax"
  | "polygon"
  | "arbitrum"
  | "optimism"
  | "base"
  | "sonic";

export type SwapChainInfo = {
  chainId: number;
  network: SwapNetwork;
};

/** Single chain from supported_chains API */
export interface SupportedChainItem {
  chain_id: number;
  chain_name: string;
  chain_key: string;
  same_chain_deposit: boolean;
  cross_chain_source: boolean;
  cross_chain_destination: boolean;
}

export interface SupportedChainsData {
  perp_vault: SupportedChainItem[];
  strategy_vault: SupportedChainItem[];
}

/** https://api.woofi.com/woofi_dex/depositor/supported_chains API response  */
export interface SupportedChainsResponse {
  status: "ok";
  data: SupportedChainsData;
}

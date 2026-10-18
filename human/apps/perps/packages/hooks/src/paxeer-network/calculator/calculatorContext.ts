import { API } from "@paxeer-network/types";
import { CalculatorCtx, CalculatorScope } from "../../types";
import { Portfolio, useAppStore } from "../appStore";
// import { useTokensInfoStore } from "../useTokensInfo/tokensInfo.store";
import { MarketCalculatorName } from "./markPrice";

export class CalculatorContext implements CalculatorCtx {
  accountInfo?: API.AccountInfo;
  symbolsInfo?: Record<string, API.SymbolExt>;
  fundingRates?: Record<string, API.FundingRate>;
  // holding: API.Holding[];
  // portfolio — dynamic getter to always read the latest output from PortfolioCalculator
  get portfolio(): Portfolio | undefined {
    return (
      this.output["portfolio"] ||
      (useAppStore.getState().portfolio as Portfolio)
    );
  }
  markPrices?: Record<string, number> | null;
  // positions: API.PositionTPSLExt[];
  // markets: Record<string, API.MarketInfoExt> | null;
  tokensInfo?: API.Token[];

  private output: Record<string, any>;

  private static _instance: CalculatorContext;

  static get instance() {
    return this._instance;
  }

  static create(scope: CalculatorScope, data: any) {
    if (!this._instance) {
      this._instance = new CalculatorContext(scope, data);
    }
    return this._instance.update(scope, data);
  }

  constructor(scope: CalculatorScope, data: any) {
    // this.accountInfo = useAppStore.getState().accountInfo as API.AccountInfo;
    // this.symbolsInfo = useAppStore.getState().symbolsInfo as Record<
    //   string,
    //   API.SymbolExt
    // >;
    this.setCtxData();

    // this.holding = useAppStore.getState().portfolio.holding as API.Holding[];

    // this.portfolio = useAppStore.getState().portfolio as Portfolio;

    // console.log("!!!! CalculatorContext", this.holding);

    // const positions = usePositionStore.getState().positions;
    // this.markPrices = scope === CalculatorScope.MARK_PRICE ? data : null;
    // this.markets = useMarketStore.getState().marketMap;

    // this.positions = usePositionStore.getState().positions[this.symbol];

    this.output = {
      // rows: positions,
    };
  }

  private update(scope: CalculatorScope, data: any) {
    this.setCtxData();
    this.markPrices =
      scope === CalculatorScope.MARK_PRICE
        ? data
        : this.output[MarketCalculatorName];
    return this;
  }

  private setCtxData() {
    this.accountInfo = useAppStore.getState().accountInfo as API.AccountInfo;
    this.symbolsInfo = useAppStore.getState().symbolsInfo as Record<
      string,
      API.SymbolExt
    >;
    this.fundingRates = useAppStore.getState().fundingRates as Record<
      string,
      API.FundingRate
    >;
    this.tokensInfo = useAppStore.getState().tokensInfo || [];
  }

  get(fn: (output: Record<string, any>) => any) {
    return fn(this.output);
  }

  getCacheValue(name: string, fallback: () => any) {
    return this.output[name] || fallback();
  }

  clearCache() {
    this.output = {};
    this.accountInfo = undefined;
  }

  deleteByName(name: string) {
    delete this.output[name];
  }

  // get positions(): API.PositionTPSLExt[] {
  //   if (this.output.positionCalculator) return this.output.positionCalculator;
  //   return usePositionStore.getState().positions;
  // }

  get isReady(): boolean {
    // return !!this.accountInfo && !!this.symbolsInfo && !!this.fundingRates;
    return !!this.accountInfo;
  }

  saveOutput(name: string, data: any) {
    this.output[name] = data;
  }

  outputToValue() {
    return this.output;
  }
}

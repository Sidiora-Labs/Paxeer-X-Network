import {PerpsDatafeed} from "@paxeer-network/paxeer-backend/core/datafeed/perps";
import type {DatafeedOptions} from "./abstract-datafeed";
/** Native paxeer-network candles and live bars; widget markup and controls are unchanged. */
export class Datafeed extends PerpsDatafeed {
 constructor(_apiUrl:string,_ws:unknown,_options?:DatafeedOptions){super();}
 resolveSymbol(name:string,onResolve:Parameters<PerpsDatafeed["resolveSymbol"]>[1],onError:Parameters<PerpsDatafeed["resolveSymbol"]>[2]){super.resolveSymbol(name.replace(/^.*:/,"").replace(/^PERP_/,"").replace(/_USDC$/,""),onResolve,onError);}
}

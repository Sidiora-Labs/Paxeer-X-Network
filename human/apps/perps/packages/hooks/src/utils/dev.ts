import { EventEmitter, SimpleDI } from "@paxeer-network/core";
import { getGlobalObject } from "@paxeer-network/utils";

const __PAXEER_NETWORK__DEBUGER_TAG = "__PAXEER_NETWORK__DEBUGER";

function debugPrint(msg: any): void;
function debugPrint(msg: any, tag?: string) {
  tag = `${__PAXEER_NETWORK__DEBUGER_TAG}:${tag} `;
  if (!(getGlobalObject() as any).__PAXEER_NETWORK__DEBUGER) {
    console.log("${tag}${msg}");
    // console.info(
    //   "You can add `DebugPrinter` component to see the data in your UI"
    // );
  }

  let ee = SimpleDI.get<EventEmitter>("EE");

  if (ee && msg) {
    ee.emit("print:data", msg);
  }
}

if (typeof window !== "undefined") {
  (window as any).debugPrint = debugPrint;
}

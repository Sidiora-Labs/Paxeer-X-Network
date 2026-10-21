import { create } from "zustand";

export interface DraftOrderLines {
  /** Whether order lines should be visible on chart */
  enabled: boolean;
  orderType: "market" | "limit" | "stop-limit" | "scaled";
  isLong: boolean;
  /** Entry/limit price (for limit and stop-limit orders) */
  entryPrice: number;
  /** Take profit price */
  tpPrice: number;
  /** Stop loss price */
  slPrice: number;
  /** Whether TP/SL lines are enabled */
  tpslEnabled: boolean;
  /** Current mark price (used for market order entry reference) */
  markPrice: number;

  // Actions — called by PerpsOrderForm when user types
  setOrderType: (t: DraftOrderLines["orderType"]) => void;
  setIsLong: (v: boolean) => void;
  setEntryPrice: (p: number) => void;
  setTpPrice: (p: number) => void;
  setSlPrice: (p: number) => void;
  setTpslEnabled: (v: boolean) => void;
  setMarkPrice: (p: number) => void;
  setEnabled: (v: boolean) => void;

  // Called by Chart when user drags a line
  onChartDragEntry: (price: number) => void;
  onChartDragTp: (price: number) => void;
  onChartDragSl: (price: number) => void;

  /** Incremented when chart drag changes a price — order form subscribes to this */
  _chartDragSeq: number;
}

export const useOrderLineStore = create<DraftOrderLines>()((set) => ({
  enabled: true,
  orderType: "market",
  isLong: true,
  entryPrice: 0,
  tpPrice: 0,
  slPrice: 0,
  tpslEnabled: false,
  markPrice: 0,

  setOrderType: (t) => set({ orderType: t }),
  setIsLong: (v) => set({ isLong: v }),
  setEntryPrice: (p) => set({ entryPrice: p }),
  setTpPrice: (p) => set({ tpPrice: p }),
  setSlPrice: (p) => set({ slPrice: p }),
  setTpslEnabled: (v) => set({ tpslEnabled: v }),
  setMarkPrice: (p) => set({ markPrice: p }),
  setEnabled: (v) => set({ enabled: v }),

  onChartDragEntry: (price) =>
    set((s) => ({ entryPrice: price, _chartDragSeq: s._chartDragSeq + 1 })),
  onChartDragTp: (price) =>
    set((s) => ({ tpPrice: price, _chartDragSeq: s._chartDragSeq + 1 })),
  onChartDragSl: (price) =>
    set((s) => ({ slPrice: price, _chartDragSeq: s._chartDragSeq + 1 })),

  _chartDragSeq: 0,
}));

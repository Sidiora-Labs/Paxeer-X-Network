import type { FeeDataType } from "./feeTier.script";

export const dataSource: FeeDataType[] = [
  {
    tier: 1,
    volume_min: 0,
    volume_max: 500000,
    maker_fee: "0.025% / 0%",
    taker_fee: "0.04% / 0.05%",
  },
  {
    tier: 2,
    volume_min: 500000,
    volume_max: 1000000,
    maker_fee: "0.02% / 0%",
    taker_fee: "0.035% / 0.05%",
  },
  {
    tier: 3,
    volume_min: 1000000,
    volume_max: 5000000,
    maker_fee: "0.018% / 0%",
    taker_fee: "0.03% / 0.05%",
  },
  {
    tier: 4,
    volume_min: 5000000,
    volume_max: 20000000,
    maker_fee: "0.012% / 0%",
    taker_fee: "0.025% / 0.05%",
  },
  {
    tier: 5,
    volume_min: 20000000,
    volume_max: 50000000,
    or: "/",
    maker_fee: "0.009% / 0%",
    taker_fee: "0.022% / 0.05%",
  },
  {
    tier: 6,
    volume_min: 50000000,
    volume_max: 100000000,
    or: "/",
    maker_fee: "0.005% / 0%",
    taker_fee: "0.018% / 0.05%",
  },
  {
    tier: 7,
    volume_min: 100000000,
    volume_max: undefined,
    or: "/",
    maker_fee: "0% / 0%",
    taker_fee: "0.015% / 0.05%",
  },
];

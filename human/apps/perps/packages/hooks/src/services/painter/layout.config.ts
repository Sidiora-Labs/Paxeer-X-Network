import { type PosterLayoutConfig } from "./basePaint";

export const DefaultLayoutConfig: PosterLayoutConfig = {
  domain: {
    fontSize: 13,
    // color: undefined,
    textBaseline: "bottom",
    position: {
      left: 20,
      bottom: 32,
    },
  },
  message: {
    fontSize: 20,
    color: "rgba(255, 255, 255, 0.98)",
    textBaseline: "top",
    position: {
      left: 20,
      top: 16,
    },
  },
  position: {
    fontSize: 14,
    color: "rgba(255,255,255,0.98)",
    position: {
      left: 20,
      top: 70,
    },
  },
  unrealizedPnl: {
    fontSize: 36,
    color: "rgba(255,255,255,0.5)",
    secondaryColor: "rgba(255,255,255,0.54)",
    secondaryFontSize: 20,
    position: {
      left: 20,
      top: 110,
    },
  },
  informations: {
    fontSize: 12,
    color: "rgba(255, 255, 255, 0.8)",
    labelColor: "rgba(255,255,255,0.36)",
    position: {
      left: 20,
      top: 150,
    },
  },

  updateTime: {
    fontSize: 10,
    color: "rgba(255,255,255,0.3)",
    textAlign: "start",
    textBaseline: "bottom",
    position: {
      left: 20,
      bottom: 17,
    },
  },
};

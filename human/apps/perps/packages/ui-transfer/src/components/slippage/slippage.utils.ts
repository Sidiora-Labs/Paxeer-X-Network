export type SlippageRiskLevel = "minimum" | "low" | "high" | undefined;

export const getSlippageRiskLevel = (
  value: number | undefined,
  min: number,
  recommendedValue: number,
): SlippageRiskLevel => {
  if (value === undefined || !Number.isFinite(value)) {
    return undefined;
  }
  if (value < min) {
    return "minimum";
  }
  if (value < recommendedValue) {
    return "low";
  }
  if (value > recommendedValue) {
    return "high";
  }
  return undefined;
};

export const shouldDisableSlippageSave = (
  value: number | undefined,
  riskLevel: SlippageRiskLevel,
) => !value || riskLevel === "minimum";

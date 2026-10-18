export enum AssetHistoryStatusEnum {
  /** @deprecated, this status is not used */
  NEW = "NEW",
  PENDING = "PENDING",
  CONFIRM = "CONFIRM",
  PROCESSING = "PROCESSING",
  COMPLETED = "COMPLETED",
  FAILED = "FAILED",
  PENDING_REBALANCE = "PENDING_REBALANCE",
}

export enum AssetHistorySideEnum {
  DEPOSIT = "DEPOSIT",
  WITHDRAW = "WITHDRAW",
}

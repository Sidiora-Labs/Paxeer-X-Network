import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { OrderValidationItem } from "./interface";

export class OrderValidation {
  static getLabel(key: keyof PaxeerNetworkOrder) {
    switch (key) {
      case "quantity":
      case "order_quantity":
        return "Quantity";
      case "order_price":
        return "Price";
      case "trigger_price":
        return "Trigger price";
      case "tp_trigger_price":
        return "TP price";
      case "sl_trigger_price":
        return "SL price";
      case "tp_order_price":
        return "TP order price";
      case "sl_order_price":
        return "SL order price";
      case "start_price":
        return "Start price";
      case "end_price":
        return "End price";
      case "total_orders":
        return "Total orders";
      case "skew":
        return "Skew";
      default:
        return key;
    }
  }

  static required(key: keyof PaxeerNetworkOrder) {
    return {
      type: "required",
      message: `${this.getLabel(key)} is required`,
    } as OrderValidationItem;
  }

  static priceErrorMin(key: keyof PaxeerNetworkOrder) {
    return {
      type: "priceErrorMin",
      message: `Your ${this.getLabel(key)} should be set lower than your order price.`,
    } as OrderValidationItem;
  }
  static priceErrorMax(key: keyof PaxeerNetworkOrder) {
    return {
      type: "priceErrorMax",
      message: `Your ${this.getLabel(key)} should be set higher than your order price.`,
    } as OrderValidationItem;
  }

  static min(key: keyof PaxeerNetworkOrder, value: number | string) {
    return {
      type: "min",
      message: `${this.getLabel(key)} must be greater than ${value}`,
      value,
    } as OrderValidationItem;
  }

  static max(key: keyof PaxeerNetworkOrder, value: number | string) {
    return {
      type: "max",
      message: `${this.getLabel(key)} must be less than ${value}`,
      value,
    } as OrderValidationItem;
  }

  static range(
    key: keyof PaxeerNetworkOrder,
    min?: number | string,
    max?: number | string,
  ) {
    return {
      type: "range",
      message: `${this.getLabel(key)} must be in the range of ${min} to ${max}`,
      min,
      max,
    } as OrderValidationItem;
  }
}

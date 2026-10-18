import { API, WSMessage } from "@paxeer-network/types";
import { BaseMergeHandler } from "./baseMergeHandler";
import { object2underscore } from "../../utils/ws";

export class RegularOrderMergeHandler extends BaseMergeHandler<
  WSMessage.Order,
  API.Order
> {
  get orderId(): number {
    return this.data.order_id;
  }
  get status(): string {
    return this.data.status;
  }
  pre(
    message: WSMessage.Order,
    prevData?: API.OrderResponse[] | undefined
  ): API.Order {
    return object2underscore(message) as unknown as API.Order;
  }
  isFullFilled(): boolean {
    return (
      "total_executed_quantity" in this.data &&
      this.data.total_executed_quantity === this.data.quantity
    );
  }
}

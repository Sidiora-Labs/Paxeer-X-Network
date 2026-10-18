import { OrderFactory } from "../factory";
import { LimitOrderCreator } from "../limitOrderCreator";
import { OrderType } from "@paxeer-network/types"; // Import the OrderType enum from the correct file
import { MarketOrderCreator } from "../marketOrderCreator";
import { IOCOrderCreator } from "../iocCreator";
import { GeneralOrderCreator } from "../generalCreator";
import { StopLimitOrderCreator } from "../stopLimitOrderCreator";
import { PostOnlyOrderCreator } from "../postOnlyCreator";
import { StopMarketOrderCreator } from "../stopMarketOrderCreator";
import { FOKOrderCreator } from "../fokCreator";

describe("orderCreator", () => {
  test("get order creator: LimitOrderCreator", () => {
    const creator = OrderFactory.create(OrderType.LIMIT); // Pass the OrderType enum as an argument
    expect(creator).toBeInstanceOf(LimitOrderCreator);
  });

  // test MarketOrderCreator, IOCOrderCreator, FOKOrderCreator, PostOnlyOrderCreator, StopLimitOrderCreator, StopMarketOrderCreator, GeneralOrderCreator
  test("get order creator: MarketOrderCreator", () => {
    const creator = OrderFactory.create(OrderType.MARKET);
    expect(creator).toBeInstanceOf(MarketOrderCreator);
  });

  test("get order creator: IOCOrderCreator", () => {
    const creator = OrderFactory.create(OrderType.IOC);
    expect(creator).toBeInstanceOf(IOCOrderCreator);
  });

  test("get order creator: FOKOrderCreator", () => {
    const creator = OrderFactory.create(OrderType.FOK);
    // expect(creator).toBeInstanceOf(FOKOrderCreator);
    expect(creator).toBeInstanceOf(FOKOrderCreator);
  });

  test("get order creator: PostOnlyOrderCreator", () => {
    const creator = OrderFactory.create(OrderType.POST_ONLY);
    // expect(creator).toBeInstanceOf(PostOnlyOrderCreator);
    expect(creator).toBeInstanceOf(PostOnlyOrderCreator);
  });

  test("get order creator: StopLimitOrderCreator", () => {
    const creator = OrderFactory.create(OrderType.STOP_LIMIT);
    expect(creator).toBeInstanceOf(StopLimitOrderCreator);
  });

  test("get order creator: StopMarketOrderCreator", () => {
    const creator = OrderFactory.create(OrderType.STOP_MARKET);
    expect(creator).toBeInstanceOf(StopMarketOrderCreator);
  });

  test("get order creator: GeneralOrderCreator", () => {
    const creator = OrderFactory.create("someOtherOrderType" as any); // Pass a non-existent order type
    expect(creator).toBeInstanceOf(GeneralOrderCreator);
  });
});

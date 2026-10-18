import {IBrokerConnectionAdapterHost} from '../type';
import useBroker from '../hooks/useBroker';
import { SideType, AlgoType, OrderCombinationType} from "../type";
import { withoutExchangePrefix } from '../../utils/chart.util';
import { OrderType } from "@paxeer-network/types";

const getBrokerAdapter = (host: IBrokerConnectionAdapterHost, broker: ReturnType<typeof useBroker>) => {
    let symbol: string;
    let _symbolInfo: any;
    const getOrderCombinationType = (orderType: any) => {
        return [
            null,
            'limit',
            OrderCombinationType.MARKET,
            OrderCombinationType.STOP_MARKET,
            OrderCombinationType.STOP_LIMIT,
        ][orderType];
    };

    return {
        symbolInfo: async (symbol: string): Promise<any> => {
            _symbolInfo = broker.getSymbolInfo(withoutExchangePrefix(symbol));

            return {
                qty: {
                    min: _symbolInfo?.baseMin ?? 0,
                    max: _symbolInfo?.baseMax ?? 0,
                    step: _symbolInfo?.baseTick ?? 0,
                },
                pipValue: 0,
                pipSize: _symbolInfo?.quoteTick ?? 0,
                minTick: _symbolInfo?.quoteTick ?? 0,
                description: '',
            };
        },
        placeOrder: async (order: any) => {
            const side = [SideType.SELL, SideType.BUY][+(order.side > 0)];
            const quantity = order.qty.toString();
            const limitPrice = (order.limitPrice ?? 0).toString();
            const triggerPrice = (order.stopPrice ?? 0).toString();
            const symbol = withoutExchangePrefix(order.symbol);
            const orderCombinationType = getOrderCombinationType(order.type);
            console.log('-- order orderCombinationType ', orderCombinationType);
            console.log('-- side, orderquantity ',side, quantity, limitPrice, triggerPrice, symbol);

            if (orderCombinationType === OrderCombinationType.MARKET) {
                console.log('--0 order', order);
                // @ts-ignore
                broker.sendMarketOrder({ side, order_quantity: quantity, symbol, order_type: OrderType.MARKET });
            } else if (orderCombinationType === OrderCombinationType.LIMIT) {
                // broker.sendLimitOrder({ side, orderQuantity: quantity, orderPrice: limitPrice, symbol });
            }
        },
        orders: () => [],
        positions: () => [],
        executions: () => [],
        connectionStatus: () => 1,
        chartContextMenuActions: (context: any) => host.defaultContextMenuActions(context),
        isTradable: async () => true,
        accountManagerInfo: () => ({}),
        currentAccount: () => '1',
        accountsMetainfo: async () => [{ id: '1' }],
        remove: () => host?.silentOrdersPlacement().unsubscribe(),
    }
}

export default getBrokerAdapter;
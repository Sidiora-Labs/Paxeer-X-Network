'use client';
import { useState, useSyncExternalStore } from 'react';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type { Operations } from '@kindle/sdk';
import { ListOrdered, LogIn } from 'lucide-react';
import { Button, EmptyState, ErrorState, LoadMore, ModalFrame, NoticeBar, SortHeader, Skeleton, UnavailableState } from '@/ui/primitives';
import { openAuthModal } from '@/shell/auth-modal-store';
import { queryKeys } from '@/core/query-keys';
import { useSessionOperations } from '@/core/market/operations';
import { isSignedOutError, toUnsupported } from '@/core/market/unsupported';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { exactPriceRatio } from '@/page-modules/token/market-presentation';
import { accountSession, subscribeAccount, type AccountSession } from '@/widgets/profile/account-transport';
import type { Order, OrderCancelResponse, OrderListResponse } from '../../../../../../packages/production-contracts/src/types.gen';
import { userFacingMessage } from '@/core/user-facing-error';

export const orderListOperation = 'IF_ORDER_LIST GET /v2/me/orders';
export const orderCancelOperation = 'IF_ORDER_CANCEL POST /v2/me/orders/{orderId}:cancel';
const orderPage = 100;
const noSession = () => null;

/** Private-account key shape shared with usePrivateAccountQuery so sign-out and epoch changes purge it. */
export function terminalOrdersKey(scope: AccountSession) {
  return queryKeys.terminalOrders(scope.session.sessionId, scope.session.wallet, scope.epoch);
}

export function ordersForPool(orders: readonly Order[], poolAddress: string): Order[] {
  const target = poolAddress.toLowerCase();
  return orders.filter(order => order.poolAddress.toLowerCase() === target);
}

export function readActiveOrders(operations: Operations, cursor: string | null, signal?: AbortSignal): Promise<OrderListResponse> {
  return operations.call('IF_ORDER_LIST', { query: { status: 'active', limit: orderPage, ...(cursor ? { cursor } : {}) } }, { signal });
}

/** IF_ORDER_CANCEL with a fresh idempotency key per confirmed request. */
export function cancelOrder(operations: Operations, orderId: string, idempotencyKey: string = crypto.randomUUID()): Promise<OrderCancelResponse> {
  return operations.call('IF_ORDER_CANCEL', { path: { orderId } }, { headers: { 'Idempotency-Key': idempotencyKey } });
}

function triggerLabel(order: Order): string {
  if (!order.triggerPrice) return '-';
  const { numerator, denominator } = order.triggerPrice;
  return /^(0|[1-9][0-9]*)$/.test(numerator) && /^[1-9][0-9]*$/.test(denominator) ? exactPriceRatio(numerator, denominator) : '-';
}

function OrdersList({ operations, scope, poolAddress }: { operations: Operations; scope: AccountSession; poolAddress: string }) {
  const client = useQueryClient();
  const key = terminalOrdersKey(scope);
  const query = useInfiniteQuery({
    queryKey: key,
    queryFn: ({ pageParam, signal }) => readActiveOrders(operations, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: last => last.nextCursor,
    retry: false,
    staleTime: 10_000,
  });
  const [pending, setPending] = useState<Order | null>(null);
  const [signNotice, setSignNotice] = useState<string | null>(null);
  const cancel = useMutation({
    mutationFn: (order: Order) => cancelOrder(operations, order.orderId),
    onSuccess: response => {
      setPending(null);
      setSignNotice(response.data.transactionRequest ? 'Your order is almost cancelled. Finish cancelling it from your wallet.' : null);
      void client.invalidateQueries({ queryKey: key });
    },
  });

  if (query.status === 'pending') return <Skeleton variant="rows" rows={3} label="orders" />;
  if (query.status === 'error') {
    if (isSignedOutError(query.error)) return <SignedOutState icon={LogIn} title="Sign in to see your orders" body="Your session ended. Sign in again to read your orders." />;
    const marker = toUnsupported(orderListOperation, query.error);
    if (marker) return <UnavailableState title="Orders" operation={marker.operation} status={marker.status} message={marker.message} onRetry={() => { void query.refetch(); }} />;
    return <ErrorState variant="inline" title="Orders can't load right now" message={userFacingMessage(query.error)} onRetry={() => { void query.refetch(); }} />;
  }
  const orders = ordersForPool(query.data.pages.flatMap(page => page.data.items), poolAddress);
  const cancelMarker = cancel.isError ? toUnsupported(orderCancelOperation, cancel.error) : null;
  return (
    <div className="flex flex-col gap-1">
      {signNotice ? <NoticeBar tone="warning" announce onDismiss={() => setSignNotice(null)}>{signNotice}</NoticeBar> : null}
      {orders.length === 0
        ? <EmptyState icon={ListOrdered} title={query.hasNextPage ? 'No open orders found yet' : 'No open orders'} body={query.hasNextPage ? 'Load more to keep looking.' : 'Orders you place on this coin show up here.'} />
        : (
          <div className="overflow-x-auto">
            
            <table aria-label="Your open orders" className="w-full min-w-[560px] text-xs [&_td]:whitespace-nowrap [&_td]:px-3">
              <thead className="bg-surface-1">
                <tr>
                  <SortHeader className="h-6" label="Type" />
                  <SortHeader className="h-6" label="Amount (raw)" align="right" />
                  <SortHeader className="h-6" label="Trigger" align="right" />
                  <SortHeader className="h-6" label="Status" />
                  <SortHeader className="h-6" label="Order" />
                  <SortHeader className="h-6" label="" align="right" />
                </tr>
              </thead>
              <tbody>
                {orders.map(order => (
                  <tr key={order.orderId} data-order-id={order.orderId} className="h-9 bg-surface-1 transition-colors hover:bg-surface-hover">
                    <td className="font-bold capitalize text-text">{order.kind}</td>
                    <td data-numeric className="text-right font-mono tabular-nums text-text" title="Amount in raw units">{order.inputRaw}</td>
                    <td data-numeric className="text-right tabular-nums text-text-muted">{triggerLabel(order)}</td>
                    <td className="capitalize text-text-muted">{order.status}</td>
                    <td className="font-mono text-text-faint" title={order.orderId}>{order.orderId.slice(0, 8)}</td>
                    <td className="text-right"><Button size="sm" variant="danger-text" onClick={() => { cancel.reset(); setPending(order); }}>Cancel</Button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      {query.hasNextPage ? <LoadMore auto={false} hasMore loading={query.isFetchingNextPage} error={query.isFetchNextPageError} label="Load more orders" onLoadMore={() => { void query.fetchNextPage(); }} /> : null}
      <ModalFrame
        open={pending !== null}
        onClose={() => { if (!cancel.isPending) setPending(null); }}
        closeOnScrim={false}
        size="sm"
        title="Cancel this order?"
        footer={(
          <div className="flex justify-end gap-2">
            <Button variant="secondary" size="sm" disabled={cancel.isPending} onClick={() => setPending(null)}>Keep order</Button>
            <Button variant="danger" size="sm" loading={cancel.isPending} onClick={() => { if (pending) cancel.mutate(pending); }}>Cancel order</Button>
          </div>
        )}
      >
        {pending ? <p className="text-sm text-text-muted">The {pending.kind} order <span className="font-mono text-text">{pending.orderId.slice(0, 8)}</span> will be withdrawn.</p> : null}
        {cancelMarker ? <UnavailableState variant="inline" title="Order cancellation" operation={cancelMarker.operation} status={cancelMarker.status} /> : null}
        {cancel.isError && !cancelMarker ? <p role="alert" className="mt-2 text-sm text-danger">The order could not be cancelled right now. {userFacingMessage(cancel.error)}</p> : null}
      </ModalFrame>
    </div>
  );
}

/** Orders tab: the signed-in wallet's active orders for this pool, with confirmed cancellation. */
export function OrdersPanel({ poolAddress }: { poolAddress: string }) {
  const operations = useSessionOperations();
  const scope = useSyncExternalStore(subscribeAccount, accountSession, noSession);
  if (!operations || !scope) {
    return <EmptyState icon={LogIn} title="Sign in to see your orders" body="Orders are read from your signed-in account." action={<Button size="sm" onClick={() => openAuthModal('Sign in to see your orders')}>Sign in</Button>} />;
  }
  return <OrdersList operations={operations} scope={scope} poolAddress={poolAddress} />;
}

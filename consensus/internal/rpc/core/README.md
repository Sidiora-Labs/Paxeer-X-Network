# Tendermint RPC

The route table is in [`routes.go`](./routes.go); the request and response types are in [`../../../rpc/coretypes`](../../../rpc/coretypes/).

## Pagination

Requests that return multiple items will be paginated to 30 items by default.
You can specify further pages with the ?page parameter. You can also set a
custom page size up to 100 with the ?per_page parameter; the cap does not apply
when the RPC `unsafe` option is enabled.

## Subscribing to events

The user can subscribe to events emitted by the engine over WebSocket, using
`/subscribe`. An error is returned if the maximum number of clients
(`max-subscription-clients`) is reached, if the client has too many
subscriptions (`max-subscriptions-per-client`), or if the query is longer than
512 bytes. The subscription timeout is 5 sec. Each subscription has a buffer of
100 events to accommodate short bursts of events or some slowness in clients.
If the subscription is terminated by the publisher, the client receives the
error "subscription terminated by publisher". The user can unsubscribe using
either `/unsubscribe` or `/unsubscribe_all`.

---
domain: plugin-core
docType: reference
package: "@paxeer-network/plugin-core"
intentTags:
  [injectable, interceptor, target-path, trading-layout, component-anchor]
lang: en
---

# Injectable intercept targets (reference)

Interceptor `target` strings must **match exactly** (case-sensitive). For concepts, integration, and interceptor patterns see the **[Plugin development guide](./GUIDE.md)**.

| Target path                         | Description                             |
| ----------------------------------- | --------------------------------------- |
| `Layout.MainMenus`                  | Main navigation menu area               |
| `Account.AccountMenu`               | Desktop account menu                    |
| `Account.MobileAccountMenu`         | Mobile account menu                     |
| `OrderBook.Desktop.Bids`            | Desktop order book bids column          |
| `OrderBook.Desktop.Asks`            | Desktop order book asks column          |
| `Trading.Layout.Desktop`            | Desktop trading page layout container   |
| `Trading.Page`                      | Trading page root                       |
| `OrderEntry`                        | Order entry root                        |
| `Trading.OrderEntry.TypeTabs`       | Order entry type tabs area              |
| `Trading.OrderEntry.BuySellSwitch`  | Order entry buy/sell switch area        |
| `Trading.OrderEntry.Available`      | Order entry available balance row       |
| `Trading.OrderEntry.QuantitySlider` | Order entry quantity slider area        |
| `Trading.OrderEntry.SubmitSection`  | Order entry submit + asset info section |
| `TradingView.Desktop`               | Desktop TradingView chart               |
| `Trading.SymbolInfoBar.Desktop`     | Desktop symbol info bar                 |
| `Table.EmptyDataIdentifier`         | Table empty state                       |
| `Deposit.DepositForm`               | Deposit form                            |
| `Transfer.DepositAndWithdraw`       | Deposit & withdraw tab container        |

## Adding targets yourself (fork the SDK)

Official npm packages only register the paths above via `injectable`. If you need more anchors, **fork this repo (or publish a private fork)** and add injection points in the relevant UI packages.

Implementation notes (from **[TECH.md](./TECH.md)** §3.3.1, path-based component interceptors):

1. **One path string for both sides**  
   The SDK pairs **`injectable` (at definition site)** with **`useInjectedComponent` (at render site)**. Whatever pattern you use, the plugin interceptor `target` **must equal** that path string exactly.

2. **Definition site: `injectable` wrapper**  
   Wrap the internal implementation and assign a path; export the wrapped component so every import goes through interceptor resolution (see `injectable` pseudocode and `PriceInput` example in TECH).

3. **Render site: `useInjectedComponent(name, DefaultComponent)`**  
   Call the hook where the UI is actually rendered: first argument is the path `name`, second is the default implementation (already `injectable` or inline). With no interceptors, the default renders directly (see `OrderEntry` + `SubmitButton` / `FeesView` examples in TECH).

4. **Naming**  
   Prefer **dot-separated namespaces** (e.g. `OrderEntry.SubmitButton`) for documentation and domain-scoped interception (TECH summary table, “Naming”).

5. **Implementation and performance**  
   The component type returned from `useInjectedComponent` must stay referentially stable when dependencies are unchanged—use `useMemo` internally or subtrees remount (e.g. inputs lose focus). Forward `props` correctly along the chain (TECH summary: “Performance”, “Props forwarding”).

For architecture diagrams, Context/Provider relationships, and registration examples see **[TECH.md](./TECH.md)**; for day-to-day plugin authoring, prefer **[GUIDE.md](./GUIDE.md)**.

> **Maintenance**: When adding `injectable(Component, "path")` in this repo, update the **Target** table above.

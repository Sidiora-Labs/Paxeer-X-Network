# layout/layout.ui.tsx

## Responsibility of AffiliateLayout

Renders the affiliate layout: Scaffold with a left sidebar. LeftSidebar wraps SideBar (from ui-scaffold) with a bordered Box. Main content is props.children inside a Box with margin. Props extend SideBarProps.

## AffiliateLayout Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| (SideBarProps) | — | — | items, current, onItemSelect, etc. |
| children | ReactNode | Yes | Main content |

## Dependencies

- react
- @paxeer-network/ui (Box)
- @paxeer-network/ui-scaffold (Scaffold, SideBar, SideBarProps)

## AffiliateLayout Example

Used internally by AffiliateLayoutWidget; not typically imported directly.

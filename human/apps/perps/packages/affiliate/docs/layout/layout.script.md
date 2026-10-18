# layout/layout.script.tsx

## Responsibility of useLayoutBuilder

Returns SideBarProps: sidebar items (Trading, Affiliate with href and icon), current path state, and onItemSelect to set current. Uses useTranslation for labels. Used by AffiliateLayoutWidget.

## Return Type

SideBarProps (from @paxeer-network/ui-scaffold): items (name, href, icon), current, onItemSelect.

## Dependencies

- react (useMemo, useState)
- @paxeer-network/i18n (useTranslation)
- @paxeer-network/ui-scaffold (SideBarProps)

## useLayoutBuilder Example

Used internally by AffiliateLayoutWidget.

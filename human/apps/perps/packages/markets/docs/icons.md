# icons.tsx

## icons.tsx Responsibilities

Exports SVG icon components used by the markets UI: edit, trash, all markets, new listings, favorites (filled/outline), circle plus, search, move to top, paxeer-network logo, add/active add, expand/collapse, filter, triangle down, arrows, sort (neutral/asc/desc), delete. All accept standard SVG props and optional className.

## icons.tsx Exports (Components)

| Name | Role | Description |
|------|------|-------------|
| EditIcon | FC<SVGProps<SVGSVGElement>> | Pencil/edit icon |
| TrashIcon | FC | Trash/delete icon |
| AllMarketsIcon | FC | Bar-chart style “all markets” icon |
| NewListingsIcon | FC | New listings icon |
| FavoritesIcon | FC | Filled star (favorite), orange tint |
| UnFavoritesIcon | FC | Outline star (unfavorite), hover orange |
| CirclePlusIcon | FC | Circle with plus |
| SearchIcon | FC | Magnifier |
| MoveToTopIcon | FC | Move to top arrow |
| PaxeerNetworkIcon | FC | paxeer-network logo mark |
| FavoritesIcon2 | FC | Smaller filled star |
| UnFavoritesIcon2 | FC | Smaller outline star |
| TopIcon | FC | Top/up arrow |
| DeleteIcon | FC | X/delete |
| AddIcon | FC | Plus |
| ActiveAddIcon | FC | Plus with brand gradient |
| ExpandIcon | FC | Expand arrows |
| CollapseIcon | FC | Collapse arrows |
| FilterIcon | FC | Filter funnel |
| TriangleDownIcon | FC | Triangle down |
| ArrowLeftIcon | FC | Left arrow |
| ArrowRightIcon | FC | Right arrow |
| SortingIcon | FC | Neutral sort (both arrows) |
| AscendingIcon | FC | Ascending sort highlight |
| DescendingIcon | FC | Descending sort highlight |

## Props (All Icon Components)

All icon components accept `SVGProps<SVGSVGElement>` (e.g. width, height, className, fill). Some (e.g. FavoritesIcon, UnFavoritesIcon) apply fixed classNames for color.

## Dependencies

- **Upstream**: `react`, `@paxeer-network/ui` (cn).
- **Downstream**: Markets list, favorites, funding, and sheet components.

## icons.tsx Example

```tsx
import {
  SearchIcon,
  FavoritesIcon,
  UnFavoritesIcon,
  ExpandIcon,
  CollapseIcon,
} from "@paxeer-network/markets";

<SearchIcon className="oui-w-4 oui-h-4" />
<FavoritesIcon />
<ExpandIcon aria-label="Expand" />
```

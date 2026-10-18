// @coord dropped @blockscout/bens-types — external package not pulled in.
// BENS filter/sort shapes are inlined to the fields that trading-app consumers
// will actually send. Paxeer uses the `.pax` TLD; `name` / `address_hash` /
// `resolved_to` / `owned_by` remain plain strings — Agent 2 handles the
// runtime mapping between user input and the name-service contract at
// 0xe2c4c3980e2436045ac5a2f14e63550906072e0b.

export interface EnsAddressLookupFilters {
  address?: string;
  resolved_to?: string;
  owned_by?: string;
  only_active?: boolean;
  protocols: Array<string> | undefined;
}

export interface EnsDomainLookupFilters {
  name?: string;
  only_active?: boolean;
  protocols: Array<string> | undefined;
}

export interface EnsLookupSorting {
  sort: 'registration_date';
  order: 'asc' | 'desc';
}

export type EnsDomainLookupFiltersOptions = Array<'resolved_to' | 'owned_by' | 'with_inactive'>;

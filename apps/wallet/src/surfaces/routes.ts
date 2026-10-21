export type SurfaceId = 'exchange' | 'bridge' | 'launchpad' | 'fees' | 'web-data';

export interface SurfaceRoute {
    readonly id: SurfaceId;
    readonly href: `/surfaces/${SurfaceId}`;
    readonly label: string;
    readonly description: string;
}

export const SURFACE_ROUTES: readonly SurfaceRoute[] = [
    { id: 'exchange', href: '/surfaces/exchange', label: 'Exchange', description: 'Orders and margin' },
    { id: 'bridge', href: '/surfaces/bridge', label: 'Bridge', description: 'Move assets out' },
    { id: 'launchpad', href: '/surfaces/launchpad', label: 'Launchpad', description: 'Launch and trade tokens' },
    { id: 'fees', href: '/surfaces/fees', label: 'Fees', description: 'Pay gas in PAX or SID' },
    { id: 'web-data', href: '/surfaces/web-data', label: 'Web data', description: 'Fetch, search and 402 draws' },
];

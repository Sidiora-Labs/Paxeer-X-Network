export const SURFACE_ENV = {
    sidRate: 'NEXT_PUBLIC_PAXEER_USID_PER_PAX',
} as const;

const RATE = /^(0|[1-9][0-9]*)(?:\.([0-9]{1,18}))?$/u;

export function readSidRate(raw: string | undefined): string | null {
    const value = raw?.trim();
    if (!value || !RATE.test(value) || /^0(?:\.0+)?$/u.test(value)) return null;
    return value;
}

export function processSidRate(): string | null {
    return readSidRate(process.env.NEXT_PUBLIC_PAXEER_USID_PER_PAX);
}

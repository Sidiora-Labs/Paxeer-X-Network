'use client';
import Image from "next/image";

const ICON_MAP: Record<string, string> = {
    // Navigation / Bottom Nav
    wallet: '/wallet/ui_icons/wallet.svg',
    clock: '/wallet/ui_icons/clock.svg',
    swap: '/wallet/ui_icons/swap.svg',
    compass: '/wallet/ui_icons/globe.svg',
    settings: '/wallet/ui_icons/gear.svg',
    globe: '/wallet/ui_icons/globe.svg',

    // Arrows
    'arrow-left': '/wallet/ui_icons/arrows/east.svg',
    'arrow-right': '/wallet/ui_icons/arrows/east.svg',
    'arrow-up-down': '/wallet/ui_icons/arrows/up-down.svg',
    'arrow-up-right': '/wallet/ui_icons/arrows/up-head.svg',
    'arrow-down-left': '/wallet/ui_icons/arrows/down-right.svg',
    'chevron-down': '/wallet/ui_icons/arrows/east.svg',
    'chevron-right': '/wallet/ui_icons/arrows/east-mini.svg',

    // Actions
    plus: '/wallet/ui_icons/plus.svg',
    minus: '/wallet/ui_icons/minus.svg',
    close: '/wallet/ui_icons/cross.svg',
    x: '/wallet/ui_icons/cross.svg',
    check: '/wallet/ui_icons/check.svg',
    copy: '/wallet/ui_icons/copy.svg',
    'copy-check': '/wallet/ui_icons/copy_check.svg',
    edit: '/wallet/ui_icons/edit.svg',
    pencil: '/wallet/ui_icons/edit.svg',
    delete: '/wallet/ui_icons/delete.svg',
    trash: '/wallet/ui_icons/delete.svg',
    refresh: '/wallet/ui_icons/refresh.svg',
    search: '/wallet/ui_icons/search.svg',
    filter: '/wallet/ui_icons/filter.svg',
    sliders: '/wallet/ui_icons/filter.svg',
    share: '/wallet/ui_icons/share.svg',
    dots: '/wallet/ui_icons/dots.svg',
    'more-vertical': '/wallet/ui_icons/dots.svg',

    // Status / Info
    info: '/wallet/ui_icons/info.svg',
    warning: '/wallet/ui_icons/status/warning.svg',
    error: '/wallet/ui_icons/status/error.svg',
    success: '/wallet/ui_icons/status/success.svg',
    pending: '/wallet/ui_icons/status/pending.svg',
    verified: '/wallet/ui_icons/verified.svg',

    // Security
    shield: '/wallet/ui_icons/nft_shield.svg',
    lock: '/wallet/ui_icons/lock.svg',
    key: '/wallet/ui_icons/key.svg',

    // External
    'external-link': '/wallet/ui_icons/open-link.svg',
    link: '/wallet/ui_icons/link.svg',

    // Tokens / Finance
    flame: '/wallet/ui_icons/flame.svg',
    lightning: '/wallet/ui_icons/lightning.svg',
    zap: '/wallet/ui_icons/lightning.svg',
    star: '/wallet/ui_icons/star_outline.svg',
    'star-filled': '/wallet/ui_icons/star_filled.svg',
    tokens: '/wallet/ui_icons/tokens.svg',
    gas: '/wallet/ui_icons/gas.svg',
    transactions: '/wallet/ui_icons/transactions.svg',

    // QR
    'qr-code': '/wallet/ui_icons/qr_code.svg',

    // User
    profile: '/wallet/ui_icons/profile.svg',
    user: '/wallet/ui_icons/profile.svg',

    // Send
    send: '/wallet/ui_icons/arrows/up-head.svg',

    // Bridge
    bridge: '/wallet/ui_icons/bridge.svg',

    // Misc
    rocket: '/wallet/ui_icons/rocket.svg',
    hexagon: '/wallet/ui_icons/hexagon.svg',
    hourglass: '/wallet/ui_icons/hourglass.svg',
    collection: '/wallet/ui_icons/collection.svg',
    apps: '/wallet/ui_icons/apps.svg',
    explorer: '/wallet/ui_icons/explorer.svg',
    block: '/wallet/ui_icons/block.svg',
};

const ROTATION_MAP: Record<string, string> = {
    'arrow-left': 'rotate-180',
    'chevron-down': 'rotate-90',
    'arrow-down-left': 'rotate-180',
};

interface SvgIconProps {
    name: string;
    className?: string;
    style?: React.CSSProperties;
}

export function SvgIcon({ name, className = 'w-4 h-4', style }: SvgIconProps) {
    const src = ICON_MAP[name];
    if (!src) {
        return <span className={className} />;
    }
    const rotation = ROTATION_MAP[name] || '';
    return (
        <Image
            src={src}
            alt=""
            width={24}
            height={24}
            className={`${className} ${rotation} inline-block`}
            style={{ ...style, filter: 'brightness(0) invert(1)' }}
            draggable={false}
        />
    );
}

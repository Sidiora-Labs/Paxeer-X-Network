'use client';
import Image from "next/image";

const ICON_MAP: Record<string, string> = {
    // Navigation / Bottom Nav
    wallet: '/ui_icons/wallet.svg',
    clock: '/ui_icons/clock.svg',
    swap: '/ui_icons/swap.svg',
    compass: '/ui_icons/globe.svg',
    settings: '/ui_icons/gear.svg',
    globe: '/ui_icons/globe.svg',

    // Arrows
    'arrow-left': '/ui_icons/arrows/east.svg',
    'arrow-right': '/ui_icons/arrows/east.svg',
    'arrow-up-down': '/ui_icons/arrows/up-down.svg',
    'arrow-up-right': '/ui_icons/arrows/up-head.svg',
    'arrow-down-left': '/ui_icons/arrows/down-right.svg',
    'chevron-down': '/ui_icons/arrows/east.svg',
    'chevron-right': '/ui_icons/arrows/east-mini.svg',

    // Actions
    plus: '/ui_icons/plus.svg',
    minus: '/ui_icons/minus.svg',
    close: '/ui_icons/cross.svg',
    x: '/ui_icons/cross.svg',
    check: '/ui_icons/check.svg',
    copy: '/ui_icons/copy.svg',
    'copy-check': '/ui_icons/copy_check.svg',
    edit: '/ui_icons/edit.svg',
    pencil: '/ui_icons/edit.svg',
    delete: '/ui_icons/delete.svg',
    trash: '/ui_icons/delete.svg',
    refresh: '/ui_icons/refresh.svg',
    search: '/ui_icons/search.svg',
    filter: '/ui_icons/filter.svg',
    sliders: '/ui_icons/filter.svg',
    share: '/ui_icons/share.svg',
    dots: '/ui_icons/dots.svg',
    'more-vertical': '/ui_icons/dots.svg',

    // Status / Info
    info: '/ui_icons/info.svg',
    warning: '/ui_icons/status/warning.svg',
    error: '/ui_icons/status/error.svg',
    success: '/ui_icons/status/success.svg',
    pending: '/ui_icons/status/pending.svg',
    verified: '/ui_icons/verified.svg',

    // Security
    shield: '/ui_icons/nft_shield.svg',
    lock: '/ui_icons/lock.svg',
    key: '/ui_icons/key.svg',

    // External
    'external-link': '/ui_icons/open-link.svg',
    link: '/ui_icons/link.svg',

    // Tokens / Finance
    flame: '/ui_icons/flame.svg',
    lightning: '/ui_icons/lightning.svg',
    zap: '/ui_icons/lightning.svg',
    star: '/ui_icons/star_outline.svg',
    'star-filled': '/ui_icons/star_filled.svg',
    tokens: '/ui_icons/tokens.svg',
    gas: '/ui_icons/gas.svg',
    transactions: '/ui_icons/transactions.svg',

    // QR
    'qr-code': '/ui_icons/qr_code.svg',

    // User
    profile: '/ui_icons/profile.svg',
    user: '/ui_icons/profile.svg',

    // Send
    send: '/ui_icons/arrows/up-head.svg',

    // Bridge
    bridge: '/ui_icons/bridge.svg',

    // Misc
    rocket: '/ui_icons/rocket.svg',
    hexagon: '/ui_icons/hexagon.svg',
    hourglass: '/ui_icons/hourglass.svg',
    collection: '/ui_icons/collection.svg',
    apps: '/ui_icons/apps.svg',
    explorer: '/ui_icons/explorer.svg',
    block: '/ui_icons/block.svg',
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

export enum KlineResolution {
    RESOLUTION_1m = '1',
    RESOLUTION_3m = '3',
    RESOLUTION_5m = '5',
    RESOLUTION_15m = '15',
    RESOLUTION_30m = '30',
    RESOLUTION_60m = '60',
    RESOLUTION_120m = '120',
    RESOLUTION_240m = '240',
    RESOLUTION_480m = '480',
    RESOLUTION_720m = '720',
    RESOLUTION_D = 'D',
    RESOLUTION_1D = '1D',
    RESOLUTION_3D = '3D',
    RESOLUTION_1W = '1W',
    RESOLUTION_1M = '1M',
}
export const mapResolution = (resolution: any) => {
    let time = '1d';
    switch (resolution) {
        case KlineResolution.RESOLUTION_1m:
            time = '1m';
            break;
        case KlineResolution.RESOLUTION_3m:
            time = '3m';
            break;
        case KlineResolution.RESOLUTION_5m:
            time = '5m';
            break;
        case KlineResolution.RESOLUTION_15m:
            time = '15m';
            break;
        case KlineResolution.RESOLUTION_30m:
            time = '30m';
            break;
        case KlineResolution.RESOLUTION_60m:
            time = '1h';
            break;
        case KlineResolution.RESOLUTION_120m:
            time = '2h';
            break;
        case KlineResolution.RESOLUTION_240m:
            time = '4h';
            break;
        case KlineResolution.RESOLUTION_480m:
            time = '8h';
            break;
        case KlineResolution.RESOLUTION_720m:
            time = '12h';
            break;
        case KlineResolution.RESOLUTION_D:
        case KlineResolution.RESOLUTION_1D:
            time = '1d';
            break;
        case KlineResolution.RESOLUTION_3D:
            time = '3d';
            break;
        case KlineResolution.RESOLUTION_1W:
            time = '1w';
            break;
        case KlineResolution.RESOLUTION_1M:
            time = '1M ';
            break;
        default:
    }

    return time;
};

export const TradingViewSDKLocalstorageKey = {
    interval: "TradingviewSDK.lastUsedTimeBasedResolution",
    lineType: "TradingviewSDK.lastUsedStyle",
    displayControlSetting: "TradingviewSDK.displaySetting",
};

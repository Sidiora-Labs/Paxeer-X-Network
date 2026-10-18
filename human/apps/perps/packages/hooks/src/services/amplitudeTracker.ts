import * as amplitude from "@amplitude/analytics-browser";
import { SimpleDI } from "@paxeer-network/core";
import { EventEmitter } from "@paxeer-network/core";
import { TrackerEventName } from "@paxeer-network/types";

export enum ENVType {
  prod = "prod",
  staging = "staging",
  qa = "qa",
  dev = "dev",
}
const apiKeyMap = {
  dev: "4d6b7db0fdd6e9de2b6a270414fd51e0",
  qa: "96476b00bc2701360f9b480629ae5263",
  staging: "dffc00e003479b86d410c448e00f2304",
  prod: "3ab9ae56ed16cc57bc2ac97ffc1098c2",
};

function getAmplitudeConfig(
  env: ENVType,
  amplitudeConfig?: {
    amplitudeId?: string;
    serverZone?: amplitude.Types.ServerZoneType;
  },
): { amplitudeId: string; options: amplitude.Types.BrowserOptions } {
  if (!amplitudeConfig) {
    return {
      amplitudeId: "",
      options: {
        serverZone: "EU",
      },
    };
  }
  const { amplitudeId, serverZone } = amplitudeConfig;
  return {
    amplitudeId: amplitudeId!,
    options: serverZone
      ? {
          serverZone: serverZone as amplitude.Types.ServerZoneType,
        }
      : {},
  };
}

export class AmplitudeTracker {
  static instanceName = "amplitudeTracker";
  private enabled=false;
  private _userId: string | undefined;
  private _sdkInfoTag: string | undefined;
  private _ee = SimpleDI.get<EventEmitter>("EE");

  constructor(
    env: ENVType,
    amplitudeConfig:
      | { amplitudeId: string; serverZone?: amplitude.Types.ServerZoneType }
      | undefined,
    sdkInfo: any,
  ) {
    const { amplitudeId, options } = getAmplitudeConfig(env, amplitudeConfig);
    this.enabled=Boolean(amplitudeId);
    if(this.enabled) amplitude.init(amplitudeId!, options);
    this.setSdkInfo(sdkInfo);
    this._bindEvents();
  }

  setUserId(userId: string) {
    if(!this.enabled)return;
    if (userId === this._userId) {
      return;
    }
    amplitude.setUserId(userId);
    this._userId = userId;
  }

  setSdkInfo(sdkInfo: any) {
    if (this._sdkInfoTag && sdkInfo.address === this._sdkInfoTag) return;
    this.identify(sdkInfo);
    this._sdkInfoTag = sdkInfo.address;
  }

  identify(properties: any) {
    if(!this.enabled)return;
    const identify = new amplitude.Identify();
    Object.entries(properties).forEach(([key, value]) => {
      identify.set(key, value as string);
    });
    amplitude.identify(identify);
  }

  track(eventName: TrackerEventName, properties?: any) {
    if(!this.enabled)return;
    amplitude.track(eventName, properties);
  }

  private _bindEvents() {
    const listenKeys = Object.values(TrackerEventName);
    listenKeys.forEach((key) => {
      this._ee.addListener(key, (params = {}) => {
        if (key === TrackerEventName.trackIdentifyUserId) {
          this.setUserId(params);
        } else if (key === TrackerEventName.trackIdentify) {
          this.identify(params);
        } else if (key === TrackerEventName.trackCustomEvent) {
          const { eventName, ...rest } = params;
          if (!eventName) {
            return;
          }

          this.track(eventName, rest);
        } else {
          this.track(key, params);
        }
      });
    });
  }
}

import { FC, useEffect, useMemo, useState } from "react";
import { Avatar } from "../avatar";
import { AvatarSizeType } from "../avatar/avatar";

export type TokenIconProps = {
  size?: AvatarSizeType;
  name?: string;
  symbol?: string;
  className?: string;
  url?: string;
};

const BASE_URL = "/assets/static";

export const TokenIcon: FC<TokenIconProps> = (props) => {
  const { primaryUrl, fallbackUrl } = useMemo(() => {
    if (props.url) {
      return { primaryUrl: props.url, fallbackUrl: props.url };
    }
    let name = props.name;
    let hasBroker = false;
    let brokerId: string | undefined;
    if (typeof props.symbol === "string") {
      const arr = props.symbol.split("_");
      // PERP_BTC_USDC_brokerId -> arr[1]
      if (arr.length >= 2) {
        name = arr[1];
      }
      brokerId = arr[3]?.trim();
      hasBroker = !!brokerId;
    }
    if (!name) {
      return { primaryUrl: undefined, fallbackUrl: undefined };
    }
    const symbolUrl = `${BASE_URL}/symbol_logo/${name}.png`;
    const brokerName = hasBroker && brokerId ? `${name}_${brokerId}` : name;
    const brokerUrl = `${BASE_URL}/broker_symbol_logo/${brokerName}.png`;
    return {
      primaryUrl: hasBroker ? brokerUrl : symbolUrl,
      fallbackUrl: symbolUrl,
    };
  }, [props.name, props.symbol, props.url]);

  const [currentSrc, setCurrentSrc] = useState(primaryUrl);

  useEffect(() => {
    setCurrentSrc(primaryUrl);
  }, [primaryUrl]);

  return (
    <Avatar
      size={props.size}
      src={currentSrc}
      alt={props.name}
      className={props.className}
      onLoadingStatusChange={(status) => {
        if (status === "error" && primaryUrl !== fallbackUrl) {
          setCurrentSrc((prev) => (prev === primaryUrl ? fallbackUrl : prev));
        }
      }}
    />
  );
};

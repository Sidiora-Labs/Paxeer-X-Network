import { FC, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Button,
  CloseCircleFillIcon,
  cn,
  Input,
  ScrollArea,
  toast,
} from "@paxeer-network/ui";
import {
  PnLDisplayFormat,
  ReferralType,
  ShareEntity,
  ShareOptions,
  SharePnLOptions,
} from "../../types/types";
import { Carousel } from "../carousel";
import {
  CarouselContent,
  CarouselItem,
  Dot,
  useCarousel,
} from "../carousel/carousel";
import { Poster } from "../poster";
import { PosterRef } from "../poster/poster";
import { getPnlInfo, getPnLPosterData, savePnlInfo } from "../utils/utils";

export const MobileSharePnLContent: FC<{
  entity: ShareEntity;
  hide: any;
  baseDp?: number;
  quoteDp?: number;
  referral?: ReferralType;
  shareOptions: SharePnLOptions;
  brokerName?: string;
}> = (props) => {
  const { shareOptions } = props;
  const { t } = useTranslation();
  const localPnlConfig = getPnlInfo();

  const hasRoiAndPnl = props.entity.roi != null && props.entity.pnl != null;
  const formats: PnLDisplayFormat[] = hasRoiAndPnl
    ? ["roi_pnl", "roi", "pnl"]
    : props.entity.roi != null
      ? ["roi"]
      : props.entity.pnl != null
        ? ["pnl"]
        : [];

  const [pnlFormat, setPnlFormat] = useState<PnLDisplayFormat>(
    formats.length == 1 ? formats[0] : localPnlConfig.pnlFormat,
  );
  const [shareOption, setShareOption] = useState<Set<ShareOptions>>(
    new Set(localPnlConfig.options),
  );
  const [message, setMessage] = useState<string>(localPnlConfig.message);
  const [selectIndex, setSelectIndex] = useState(localPnlConfig.bgIndex);
  // const { shareOptions } = useTradingPageContext();
  const { backgroundImages, ...resetOptions } = shareOptions ?? {
    backgroundImages: [],
  };

  const [domain, setDomain] = useState("");
  // Temporary switch: force LTR poster rendering for now.
  // Set to true to re-enable RTL poster rendering later.
  const enableRTLPoster = false;
  const direction: "ltr" | "rtl" = enableRTLPoster ? "rtl" : "ltr";

  // TODO(hooks): Replace hook calls inside map with a stable ref collection.
  const posterRefs = shareOptions?.backgroundImages?.map(() =>
    useRef<PosterRef | null>(null),
  );

  useEffect(() => {
    const currentDomain = window.location.hostname;
    setDomain(currentDomain);
  }, []);

  const posterData = getPnLPosterData(
    props.entity,
    message,
    domain,
    pnlFormat,
    shareOption,
    (key) => t(key),
    props.baseDp,
    props.quoteDp,
    props.referral,
    props.brokerName,
  );
  // console.log("pster data", posterData, props.entity);

  const carouselRef = useRef<any>();
  const aspectRatio = 552 / 310;
  const [scale, setScale] = useState(1);
  const [carouselHeight, setCarouselHeight] = useState(0);

  const [focus, setFocus] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (carouselRef.current) {
      const divWidth = carouselRef.current.offsetWidth;
      const divHeight = divWidth / aspectRatio;
      setCarouselHeight(divHeight);
      setScale(divWidth / 552);
    }
  }, [carouselRef, domain]);

  const onSharePnL = async (
    posterRef: React.MutableRefObject<PosterRef | null>,
  ) => {
    if (!posterRef.current) return;
    const data = posterRef.current?.toDataURL();
    const blob = dataURItoBlob(data);
    try {
      // Check if the browser supports the share feature
      if (navigator.share) {
        await navigator.share({
          // title: "Share PnL",
          text: message,
          // url: imageUrl,
          files: [new File([blob], "image.png", { type: "image/png" })],
        });
        console.log("Image shared successfully!");
      } else {
        console.log("Share API is not supported in this browser.");
      }
      props.hide?.();
    } catch (error) {
      console.error("Error sharing image:", error);
    }
  };

  // check if the entity has the option, like formats
  const options: ShareOptions[] = useMemo(() => {
    const mapping: ShareOptions[] = [
      "openPrice",
      "closePrice",
      "openTime",
      "closeTime",
      "leverage",
      "markPrice",
      "quantity",
    ];

    return mapping.filter((key) => !!props.entity[key]);
  }, [props.entity]);

  savePnlInfo(pnlFormat, shareOption, selectIndex, message);

  return (
    <div className="oui-w-full">
      <div
        ref={carouselRef}
        className="oui-mt-4 oui-w-full oui-overflow-hidden"
        style={{ height: `${carouselHeight + 20}px` }}
      >
        <Carousel
          className="oui-w-full oui-overflow-hidden"
          opts={{ align: "start" }}
          initIndex={selectIndex}
        >
          <CarouselContent style={{ height: `${carouselHeight}px` }}>
            {shareOptions?.backgroundImages?.map((item, index) => (
              <CarouselItem key={index}>
                <Poster
                  className="oui-origin-top-left oui-transform"
                  style={{ scale: `${scale}` }}
                  width={552}
                  height={310}
                  data={{
                    backgroundImg: item,
                    direction,
                    ...resetOptions,
                    data: posterData,
                  }}
                  ratio={3}
                  ref={posterRefs?.[index]}
                />
              </CarouselItem>
            ))}
          </CarouselContent>
          <div className="oui-mb-1 oui-mt-2 oui-flex oui-justify-center">
            {/* TODO(theme): "oui-bg-base-300" is an invalid token (the base scale only defines 1-10) and generates no CSS, so the carousel dots have no background. Remove it or replace with "oui-bg-base-3" after visual comparison. */}
            <MyIdentifier
              dotClassName="oui-w-[16px] oui-h-[4px] oui-bg-base-300"
              dotActiveClassName="!oui-bg-primary-darken oui-w-[20px]"
              setSelectIndex={setSelectIndex}
            />
          </div>
        </Carousel>
      </div>

      {/* @ts-ignore */}
      <ScrollArea className="oui-custom-scrollbar oui-max-h-[200px] oui-overflow-y-auto">
        <div className="oui-mt-4">
          <div className="oui-text-3xs oui-text-base-contrast-54">
            {t("share.pnl.displayFormat")}
          </div>
          <div className="oui-row-span-1 oui-grid oui-grid-cols-3 oui-justify-between oui-gap-3 oui-px-1 oui-pt-3">
            {formats.map((item, index) => (
              <PnlFormatView
                key={index}
                setPnlFormat={setPnlFormat}
                type={item}
                curType={pnlFormat}
              />
            ))}
          </div>
        </div>

        <div className="oui-mt-3">
          <div className="oui-h-[18px] oui-text-3xs oui-text-base-contrast-54">
            {t("share.pnl.optionalInfo")}
          </div>
          <div className="oui-mt-3 oui-flex oui-flex-wrap oui-gap-3">
            {options.map((item, index) => (
              <ShareOption
                key={index}
                setShareOption={setShareOption}
                type={item}
                curType={shareOption}
              />
            ))}
          </div>
        </div>

        <div className="oui-mb-8 oui-mt-3">
          <div className="oui-h-[18px] oui-text-3xs oui-text-base-contrast-54">
            {t("share.pnl.optionalInfo.message")}
          </div>
          {/* TODO(theme): "oui-bg-base-600" is an invalid token (the base scale only defines 1-10) and generates no CSS, so this wrapper has no background. Remove it or replace with "oui-bg-base-6" after visual comparison. */}
          <div className="oui-bg-base-600 oui-mx-1 oui-mt-3 oui-h-[48px]">
            {/* TODO: "containerClassName" is never applied by BaseInput, so the intended transparent background and height have no effect. Remove it or switch to classNames={{ root: ... }} later. */}
            <Input
              placeholder={t("share.pnl.optionalInfo.message.placeholder")}
              containerClassName="oui-bg-transparent oui-h-[48px]"
              value={message}
              autoFocus={false}
              onChange={(e) => {
                if (e.target.value.length > 25) {
                  toast.error(t("share.pnl.optionalInfo.message.maxLength"));
                  return;
                }
                setMessage(e.target.value);
              }}
              ref={inputRef}
              onFocus={() => setFocus(true)}
              onBlur={() => setFocus(false)}
              suffix={
                focus && (
                  <button
                    className="oui-me-3 oui-cursor-pointer"
                    onMouseDown={(e) => {
                      console.log("set message to empty");

                      setMessage("");
                      setTimeout(() => {
                        inputRef.current?.focus();
                      }, 50);
                      e.stopPropagation();
                    }}
                  >
                    <CloseCircleFillIcon size={18} color="white" />
                  </button>
                )
              }
            />
          </div>
        </div>
      </ScrollArea>

      <div className="oui-pt-2">
        <Button
          fullWidth
          className="oui-h-[40px] oui-text-[16px]"
          onClick={() => {
            const ref = posterRefs?.[selectIndex];
            if (ref) {
              onSharePnL(ref);
            }
          }}
        >
          {t("common.share")}
        </Button>
      </div>
    </div>
  );
};

const PnlFormatView: FC<{
  type: PnLDisplayFormat;
  curType?: PnLDisplayFormat;
  setPnlFormat: any;
}> = (props) => {
  const { type, curType, setPnlFormat } = props;
  const { t } = useTranslation();

  const text = useMemo(() => {
    switch (type) {
      case "roi_pnl":
        return t("share.pnl.displayFormat.roi&Pnl");
      case "roi":
        return t("share.pnl.displayFormat.roi");
      case "pnl":
        return t("share.pnl.displayFormat.pnl");
    }
  }, [t, type]);

  const isSelected = type === curType;

  return (
    <div
      className={cn(
        "oui-referral-shadow oui-flex oui-h-[46px] oui-flex-1 oui-items-center oui-rounded-lg oui-bg-base-4 oui-px-3 oui-shadow-lg hover:oui-cursor-pointer",
        isSelected && "oui-dot-sel oui-bg-primary-darken",
      )}
      onClick={() => {
        setPnlFormat(type);
      }}
    >
      <div className="oui-text-sm oui-text-base-contrast">{text}</div>
      {/* {isSelected && <RadioIcon size={20} />} */}
    </div>
  );
};

const ShareOption: FC<{
  type: ShareOptions;
  curType: Set<ShareOptions>;
  setShareOption: any;
}> = (props) => {
  const { type, curType, setShareOption } = props;
  const { t } = useTranslation();

  const text = useMemo(() => {
    switch (type) {
      case "openPrice":
        return t("share.pnl.optionalInfo.openPrice");
      case "closePrice":
        return t("share.pnl.optionalInfo.closePrice");
      case "openTime":
        return t("share.pnl.optionalInfo.openTime");
      case "closeTime":
        return t("share.pnl.optionalInfo.closeTime");
      case "markPrice":
        return t("common.markPrice");
      case "quantity":
        return t("common.quantity");
      case "leverage":
        return t("common.leverage");
    }
  }, [type, t]);

  const isSelected = curType.has(type);

  return (
    <div
      className={cn(
        "oui-referral-shadow oui-mt-0 oui-flex oui-h-[46px] oui-w-[calc(50%-6px)] oui-items-center oui-rounded-lg oui-bg-base-4 oui-p-3 oui-shadow-lg hover:oui-cursor-pointer",
      )}
      onClick={() => {
        // setPnlFormat(type);
        setShareOption((value: Set<ShareOptions>) => {
          const updateSet = new Set(value);
          if (isSelected) {
            updateSet.delete(type);
          } else {
            updateSet.add(type);
          }
          return updateSet;
        });
      }}
    >
      <div className="oui-flex-1 oui-text-sm oui-text-base-contrast">
        {text}
      </div>
      {isSelected && <ChoicesFillIcon />}
    </div>
  );
};
function dataURItoBlob(dataURI: string) {
  const byteString = atob(dataURI.split(",")[1]);
  const mimeString = dataURI.split(",")[0].split(":")[1].split(";")[0];
  const ab = new ArrayBuffer(byteString.length);
  const ia = new Uint8Array(ab);
  for (let i = 0; i < byteString.length; i++) {
    ia[i] = byteString.charCodeAt(i);
  }
  return new Blob([ab], { type: mimeString });
}

const MyIdentifier: FC<{
  setSelectIndex: any;
  className?: string;
  dotClassName?: string;
  dotActiveClassName?: string;
  onClick?: (index: number) => void;
}> = (props) => {
  const { scrollSnaps, selectedIndex } = useCarousel();
  useEffect(() => {
    props.setSelectIndex(selectedIndex);
  }, [selectedIndex]);

  return (
    <div className={cn("oui-flex oui-gap-1")}>
      {scrollSnaps.map((_: any, index: number) => {
        return (
          <Dot
            key={index}
            index={index}
            active={index === selectedIndex}
            onClick={props.onClick}
            className={props.dotClassName}
            activeClassName={props.dotActiveClassName}
          />
        );
      })}
    </div>
  );
};

const ChoicesFillIcon = () => {
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M2.01416 11.9989C2.01416 6.47589 6.49136 1.9989 12.0142 1.9989C17.5372 1.9989 22.0142 6.47589 22.0142 11.9989C22.0142 17.5219 17.5372 21.9989 12.0142 21.9989C6.49136 21.9989 2.01416 17.5219 2.01416 11.9989ZM16.9853 7.31211C17.2125 7.09537 17.5236 7 17.8218 7C18.1201 7 18.4312 7.09537 18.6583 7.31211C19.1139 7.74546 19.1139 8.47384 18.6583 8.9072L10.5077 16.675C10.0534 17.1083 9.28909 17.1083 8.83472 16.675L5.34077 13.3459C4.88641 12.9126 4.88641 12.1841 5.34077 11.7508C5.79631 11.3175 6.56057 11.3175 7.01493 11.7508L9.67122 14.2822L16.9853 7.31211Z"
        className="oui-fill-base-contrast"
      />
    </svg>
  );
};

import { FC, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { EMPTY_LIST } from "@paxeer-network/types";
import { Divider, Flex, toast, Text, Box } from "@paxeer-network/ui";
import {
  PnLDisplayFormat,
  ReferralType,
  ShareEntity,
  ShareOptions,
  SharePnLOptions,
} from "../../types/types";
import { Poster } from "../poster";
import { PosterRef } from "../poster/poster";
import { getPnlInfo, getPnLPosterData, savePnlInfo } from "../utils/utils";
import { BottomButtons } from "./bottomBtns";
import { CarouselBackgroundImage } from "./carousel";
import { Message } from "./message";
import { ShareOption } from "./options";
import { PnlFormatView } from "./pnlFormat";

export const DesktopSharePnLContent: FC<{
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
  const [selectedSnap, setSelectedSnap] = useState(localPnlConfig.bgIndex);
  const [message, setMessage] = useState(localPnlConfig.message);
  const [check, setCheck] = useState(false);
  const { backgroundImages, ...resetOptions } = shareOptions ?? {
    backgroundImages: [],
  };

  const [domain, setDomain] = useState("");
  // Temporary switch: force LTR poster rendering for now.
  // Set to true to re-enable RTL poster rendering later.
  const enableRTLPoster = false;
  const direction: "ltr" | "rtl" = enableRTLPoster ? "rtl" : "ltr";

  const posterRef = useRef<PosterRef | null>(null);

  useEffect(() => {
    const currentDomain = window.location.hostname;
    setDomain(currentDomain);
  }, []);

  const curBgImg = useMemo(() => {
    return shareOptions?.backgroundImages?.[selectedSnap];
  }, [shareOptions?.backgroundImages, selectedSnap]);

  const posterData = getPnLPosterData(
    props.entity,
    check ? message : "",
    domain,
    pnlFormat,
    shareOption,
    (key) => t(key),
    props.baseDp,
    props.quoteDp,
    props.referral,
    props.brokerName,
  );

  const onCopy = () => {
    posterRef.current
      ?.copy()
      .then(() => {
        props.hide?.();
        toast.success(t("share.pnl.image.copied"));
      })
      .catch((e: any) => {
        toast.error(() => {
          return (
            <div>
              <div>{t("common.copy.failed")}</div>
              <div className="oui-mt-2 oui-max-w-[396px] oui-text-2xs oui-text-base-contrast-54">
                {t("share.pnl.copy.failed.description")}
              </div>
            </div>
          );
        });
      });
  };
  const onDownload = () => {
    posterRef.current?.download("Poster.png");
    props.hide?.();
  };

  // check if the entity has the option, like formats
  const options: ShareOptions[] = useMemo(() => {
    const mapping: ShareOptions[] = [
      "openPrice",
      "closePrice",
      "markPrice",
      "openTime",
      "closeTime",
      "leverage",
      "quantity",
    ];

    return mapping.filter((key) => !!props.entity[key]);
  }, [props.entity]);

  savePnlInfo(pnlFormat, shareOption, selectedSnap, message);

  return (
    <div className="oui-relative oui-flex oui-size-full oui-flex-col">
      <div className="oui-h-full oui-flex-1 oui-overflow-y-auto">
        <Box mt={9} height={422}>
          <Flex itemAlign={"center"} justify={"center"}>
            <Poster
              // className="oui-mx-11"
              width={552}
              height={310}
              data={{
                backgroundImg: curBgImg,
                direction,
                ...resetOptions,
                data: posterData,
              }}
              ratio={3}
              ref={posterRef}
            />
          </Flex>
          <CarouselBackgroundImage
            backgroundImages={shareOptions?.backgroundImages ?? EMPTY_LIST}
            selectedSnap={selectedSnap}
            setSelectedSnap={setSelectedSnap}
          />
        </Box>

        <Flex
          direction={"column"}
          px={10}
          mt={6}
          justify={"start"}
          itemAlign={"start"}
          width={"100%"}
        >
          <Text size="sm" intensity={80}>
            {t("share.pnl.displayFormat")}
          </Text>
          <Flex pt={3} gap={3} itemAlign={"center"}>
            {formats.map((e, index) => (
              <PnlFormatView
                key={index}
                setPnlFormat={setPnlFormat}
                type={e}
                curType={pnlFormat}
              />
            ))}
          </Flex>

          <Divider className="oui-w-full oui-pt-6 oui-border-line-10" />

          <Flex
            mt={6}
            direction={"column"}
            justify={"start"}
            itemAlign={"start"}
          >
            <Text size="sm" intensity={80}>
              {t("share.pnl.optionalInfo")}
            </Text>
            <Flex mt={3} gap={4} className="oui-flex-wrap">
              {options.map((item, index) => (
                <ShareOption
                  key={index}
                  setShareOption={setShareOption}
                  type={item}
                  curType={shareOption}
                />
              ))}
            </Flex>
          </Flex>

          <Message
            message={message}
            setMessage={setMessage}
            check={check}
            setCheck={setCheck}
          />
        </Flex>
      </div>

      <BottomButtons onClickCopy={onCopy} onClickDownload={onDownload} />
    </div>
  );
};

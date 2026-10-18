import { FC, useCallback, useState, useEffect } from "react";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  useEmblaCarousel,
} from "@paxeer-network/ui";
import { DefaultCampaign } from "../DefaultCampaign";
import { CampaignItemUI } from "../campaign.item.ui";
import { CampaignConfig } from "../type";
import { getCampaignTag } from "../utils";

export const CampaignsHeaderUI: FC<{
  campaigns: CampaignConfig[];
  currentCampaignId: string;
  onCampaignChange: (campaignId: string) => void;
  backgroundSrc?: string;
}> = ({ campaigns, currentCampaignId, onCampaignChange, backgroundSrc }) => {
  const [isLargeScreen, setIsLargeScreen] = useState(false);
  const [canScrollPrev, setCanScrollPrev] = useState(false);
  const [canScrollNext, setCanScrollNext] = useState(false);

  useEffect(() => {
    const checkScreenSize = () => {
      setIsLargeScreen(window.innerWidth >= 1024);
    };

    // Check initial size
    checkScreenSize();

    // Add event listener
    window.addEventListener("resize", checkScreenSize);

    // Cleanup
    return () => window.removeEventListener("resize", checkScreenSize);
  }, []);

  const [emblaRef, emblaApi] = useEmblaCarousel({
    loop: false,
    align: "start",
    containScroll: "keepSnaps",
    slidesToScroll: 1,
    skipSnaps: false,
    dragFree: false,
  });

  // Update scroll availability
  const updateScrollAvailability = useCallback(() => {
    if (!emblaApi || !campaigns?.length) {
      return;
    }

    setCanScrollPrev(emblaApi.canScrollPrev());

    // Check if the last slide is in view
    const slidesInView = emblaApi.slidesInView();
    const lastSlideIndex = campaigns.length - 1;
    const isLastSlideInView = slidesInView.includes(lastSlideIndex);

    setCanScrollNext(emblaApi.canScrollNext() && !isLastSlideInView);
  }, [emblaApi, campaigns]);

  useEffect(() => {
    if (!emblaApi) {
      return;
    }

    // Initial check
    updateScrollAvailability();

    // Listen for scroll events
    emblaApi.on("select", updateScrollAvailability);
    emblaApi.on("reInit", updateScrollAvailability);
    emblaApi.on("scroll", updateScrollAvailability);

    return () => {
      emblaApi.off("select", updateScrollAvailability);
      emblaApi.off("reInit", updateScrollAvailability);
      emblaApi.off("scroll", updateScrollAvailability);
    };
  }, [emblaApi, updateScrollAvailability]);

  const scrollPrev = useCallback(() => {
    if (emblaApi) emblaApi.scrollPrev();
  }, [emblaApi]);

  const scrollNext = useCallback(() => {
    if (emblaApi) emblaApi.scrollNext();
  }, [emblaApi]);

  // Calculate width considering gap (0.5rem = 8px)
  // For 2 items: (100% - 1 * gap) / 2
  // For 3 items: (100% - 2 * gap) / 3
  const slideStyle = isLargeScreen
    ? { flexBasis: "calc((100% - 2rem) / 4)", width: "calc((100% - 2rem) / 4)" }
    : {
        flexBasis: "calc((100% - 1rem) / 3)",
        width: "calc((100% - 1rem) / 3)",
      };

  // Check if scroll buttons should be hidden based on screen size and campaign count
  const shouldHideScrollButtons = isLargeScreen
    ? campaigns.length <= 4
    : campaigns.length <= 3;

  return (
    <div className="oui-flex oui-w-full oui-items-center oui-gap-2 oui-px-6">
      <div className="oui-shrink-0">
        <DefaultCampaign
          currentCampaignId={currentCampaignId}
          onCampaignChange={onCampaignChange}
          className="oui-min-w-[322px]"
        />
      </div>

      <div className="oui-h-[78px] oui-w-px oui-bg-base-contrast-16" />

      <button
        onClick={scrollPrev}
        disabled={!canScrollPrev}
        className={`oui-group oui-flex oui-h-[78px] oui-w-6 oui-shrink-0 oui-items-center oui-justify-center oui-rounded-lg oui-transition-colors hover:oui-bg-base-7 disabled:oui-cursor-not-allowed disabled:oui-opacity-30 disabled:hover:oui-bg-transparent ${shouldHideScrollButtons ? "oui-hidden" : ""}`}
        aria-label="Previous campaigns"
      >
        <ChevronLeftIcon
          opacity={1}
          className="oui-text-base-contrast-54 group-hover:oui-text-base-contrast"
        />
      </button>

      <div className="oui-min-w-0 oui-flex-1 oui-overflow-hidden">
        <div ref={emblaRef}>
          <div className="oui-flex oui-gap-2">
            {campaigns?.map((campaign) => (
              <div
                key={campaign.campaign_id}
                className="oui-shrink-0"
                style={slideStyle}
              >
                <CampaignItemUI
                  backgroundSrc={backgroundSrc}
                  campaign={campaign}
                  tag={getCampaignTag(campaign)}
                  active={currentCampaignId == campaign.campaign_id}
                  onCampaignChange={onCampaignChange}
                  classNames={{
                    tag: {
                      container: "oui-h-[23px] oui-py-1 oui-px-2",
                    },
                  }}
                />
              </div>
            ))}
          </div>
        </div>
      </div>
      <button
        onClick={scrollNext}
        disabled={!canScrollNext}
        className={`oui-group oui-flex oui-h-[78px] oui-w-6 oui-shrink-0 oui-items-center oui-justify-center oui-rounded-lg oui-transition-colors hover:oui-bg-base-7 disabled:oui-cursor-not-allowed disabled:oui-opacity-30 disabled:hover:oui-bg-transparent ${shouldHideScrollButtons ? "oui-hidden" : ""}`}
        aria-label="Next campaigns"
      >
        <ChevronRightIcon
          opacity={1}
          className="oui-text-base-contrast-54 group-hover:oui-text-base-contrast"
        />
      </button>
    </div>
  );
};

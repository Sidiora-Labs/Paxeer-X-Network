import { useCallback, useState, useEffect, memo, useRef } from "react";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  useEmblaCarousel,
} from "@paxeer-network/ui";
import { DefaultCampaign } from "../DefaultCampaign";
import { CampaignItemUI } from "../campaign.item.ui";
import { CampaignConfig } from "../type";
import { getCampaignTag } from "../utils";

export const CampaignsHeaderMobileUI = memo<{
  campaigns: CampaignConfig[];
  currentCampaignId: string;
  onCampaignChange: (campaignId: string) => void;
  backgroundSrc?: string;
}>(({ campaigns, currentCampaignId, onCampaignChange, backgroundSrc }) => {
  const [canScrollPrev, setCanScrollPrev] = useState(false);
  const [canScrollNext, setCanScrollNext] = useState(false);
  const hasInitialScrolled = useRef(false);
  const isInitializing = useRef(true);

  const [emblaRef, emblaApi] = useEmblaCarousel({
    loop: false,
    align: "start",
    containScroll: "keepSnaps",
    slidesToScroll: 1,
    skipSnaps: false,
    dragFree: false,
  });

  // All campaign items including default campaign
  const allCampaignItems = [
    { campaign_id: "general", isDefault: true },
    ...campaigns.map((campaign) => ({ ...campaign, isDefault: false })),
  ];

  // Update scroll availability and handle auto campaign change on slide
  const updateScrollAvailability = useCallback(() => {
    if (!emblaApi || !campaigns?.length) return;

    setCanScrollPrev(emblaApi.canScrollPrev());
    setCanScrollNext(emblaApi.canScrollNext());

    // Skip auto campaign change during initialization or before initial scroll is completed
    if (isInitializing.current || !hasInitialScrolled.current) return;

    // Auto trigger onCampaignChange when slide changes (only after user interaction)
    const selectedIndex = emblaApi.selectedScrollSnap();
    const selectedCampaign = allCampaignItems[selectedIndex];

    if (selectedCampaign) {
      const campaignId = selectedCampaign.isDefault
        ? "general"
        : String(selectedCampaign.campaign_id);
      if (campaignId != currentCampaignId) {
        onCampaignChange(campaignId);
      }
    }
  }, [
    emblaApi,
    campaigns,
    currentCampaignId,
    onCampaignChange,
    allCampaignItems,
  ]);

  // Auto scroll to current campaign on initial load
  const scrollToCurrentCampaign = useCallback(() => {
    if (!emblaApi || !campaigns?.length) {
      return;
    }

    const targetIndex = allCampaignItems.findIndex((item) => {
      const campaignId = item.isDefault ? "general" : String(item.campaign_id);
      return campaignId === currentCampaignId;
    });

    if (targetIndex !== -1 && targetIndex !== emblaApi.selectedScrollSnap()) {
      emblaApi.scrollTo(targetIndex, false); // false to disable animation for initial scroll
    }

    // Mark as scrolled and initialized after the scroll operation
    hasInitialScrolled.current = true;
    // Small delay to ensure the scroll operation is completed before enabling auto change
    setTimeout(() => {
      isInitializing.current = false;
    }, 100);
  }, [emblaApi, campaigns, currentCampaignId, allCampaignItems]);

  useEffect(() => {
    if (!emblaApi) {
      return;
    }

    // Initial check
    updateScrollAvailability();

    // Auto scroll to current campaign on initial load
    scrollToCurrentCampaign();

    // Listen for scroll events
    emblaApi.on("select", updateScrollAvailability);
    emblaApi.on("reInit", updateScrollAvailability);

    return () => {
      emblaApi.off("select", updateScrollAvailability);
      emblaApi.off("reInit", updateScrollAvailability);
    };
  }, [emblaApi, updateScrollAvailability, scrollToCurrentCampaign]);

  // Reset scroll flag when currentCampaignId changes externally
  useEffect(() => {
    hasInitialScrolled.current = false;
    isInitializing.current = true;
    if (emblaApi) {
      scrollToCurrentCampaign();
    }
  }, [currentCampaignId, scrollToCurrentCampaign, emblaApi]);

  const scrollPrev = useCallback(() => {
    if (emblaApi) {
      // Enable auto campaign change after user interaction
      isInitializing.current = false;
      emblaApi.scrollPrev();
    }
  }, [emblaApi]);

  const scrollNext = useCallback(() => {
    if (emblaApi) {
      // Enable auto campaign change after user interaction
      isInitializing.current = false;
      emblaApi.scrollNext();
    }
  }, [emblaApi]);

  // Mobile shows one item at full width
  const slideStyle = { flexBasis: "100%", width: "100%", height: "42px" };

  // Hide scroll buttons when there's only one or no campaigns
  const shouldHideScrollButtons = campaigns.length < 1;

  return (
    <div className="oui-flex oui-w-full oui-items-center oui-gap-2 oui-px-3">
      <button
        onClick={scrollPrev}
        disabled={!canScrollPrev}
        className={`oui-group oui-flex oui-h-[42px] oui-w-6 oui-shrink-0 oui-items-center oui-justify-center oui-rounded-lg oui-transition-colors hover:oui-bg-base-7 disabled:oui-cursor-not-allowed disabled:oui-opacity-30 disabled:hover:oui-bg-transparent ${shouldHideScrollButtons ? "oui-hidden" : ""}`}
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
            <DefaultCampaign
              key="general_campaign"
              currentCampaignId={currentCampaignId}
              onCampaignChange={onCampaignChange}
              style={slideStyle}
              className="oui-w-full oui-shrink-0"
            />
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
                    container: "!oui-h-[42px]",
                    title: "!oui-text-[10px]",
                    content: "!oui-gap-[2px]",
                    tag: {
                      container: "!oui-h-[14px] !oui-px-1 !oui-py-[2px]",
                      text: "!oui-text-[10px]",
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
        className={`oui-group oui-flex oui-h-[42px] oui-w-6 oui-shrink-0 oui-items-center oui-justify-center oui-rounded-lg oui-transition-colors hover:oui-bg-base-7 disabled:oui-cursor-not-allowed disabled:oui-opacity-30 disabled:hover:oui-bg-transparent ${shouldHideScrollButtons ? "oui-hidden" : ""}`}
        aria-label="Next campaigns"
      >
        <ChevronRightIcon
          opacity={1}
          className="oui-text-base-contrast-54 group-hover:oui-text-base-contrast"
        />
      </button>
    </div>
  );
});

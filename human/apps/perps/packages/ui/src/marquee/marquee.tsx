import React, { useMemo } from "react";
import AutoScroll from "embla-carousel-auto-scroll";
import useEmblaCarousel, {
  type UseEmblaCarouselType,
} from "embla-carousel-react";
import { cnBase } from "tailwind-variants";

// Extract types from embla carousel
type EmblaCarouselType = UseEmblaCarouselType[1];
type UseEmblaCarouselParameters = Parameters<typeof useEmblaCarousel>;
type EmblaCarouselOptions = UseEmblaCarouselParameters[0];

/**
 * Auto scroll plugin options
 */
export interface AutoScrollOptions {
  /** Scroll speed in pixels per second */
  speed?: number;
  /** Scroll direction - 'forward' or 'backward' */
  direction?: "forward" | "backward";
  /** Whether to start auto-scrolling on initialization */
  playOnInit?: boolean;
  /** Whether to stop auto-scrolling on user interaction */
  stopOnInteraction?: boolean;
  /** Whether to stop auto-scrolling on mouse enter */
  stopOnMouseEnter?: boolean;
  /** Whether to stop auto-scrolling on focus */
  stopOnFocusIn?: boolean;
  /** Delay before starting auto-scroll in milliseconds */
  startDelay?: number;
  /** Root node to listen for interactions */
  rootNode?: (emblaRoot: HTMLElement) => HTMLElement;
}

/**
 * Props for the Marquee component
 */
export interface MarqueeProps<T = unknown> {
  /** Array of data items to display in the marquee */
  data: T[] | ReadonlyArray<T>;
  /** Function to render each item */
  renderItem: (item: T, index: number) => React.ReactNode;
  /** Embla carousel options */
  carouselOptions?: EmblaCarouselOptions;
  /** Auto scroll plugin options */
  autoScrollOptions?: AutoScrollOptions;
  /** Additional CSS class name */
  className?: string;
  /** Callback to get the embla carousel API instance */
  setApi?: (api: EmblaCarouselType) => void;
}

/**
 * Marquee component using Embla Carousel with auto-scroll functionality
 *
 * @example
 * ```tsx
 * const items = ['Item 1', 'Item 2', 'Item 3'];
 *
 * <Marquee
 *   data={items}
 *   renderItem={(item, index) => <div key={index}>{item}</div>}
 *   carouselOptions={{
 *     loop: true,
 *     align: "start",
 *     axis: "x"
 *   }}
 *   autoScrollOptions={{
 *     speed: 1,
 *     direction: "forward",
 *     stopOnMouseEnter: true
 *   }}
 * />
 * ```
 */
export const Marquee = <T,>(props: MarqueeProps<T>) => {
  const {
    data,
    renderItem,
    carouselOptions = {},
    autoScrollOptions = {},
    className,
    setApi,
  } = props;

  // Determine if scrolling is horizontal or vertical based on axis option
  const isHorizontal = useMemo<boolean>(
    () => carouselOptions.axis !== "y",
    [carouselOptions.axis],
  );

  // Configure embla carousel options with sensible defaults
  const emblaOptions = useMemo<EmblaCarouselOptions>(() => {
    return {
      loop: true,
      align: "start",
      containScroll: "trimSnaps",
      skipSnaps: false,
      axis: "x",
      ...carouselOptions,
    };
  }, [carouselOptions]);

  // Configure auto-scroll plugin options with sensible defaults
  const autoScrollPluginOptions = useMemo(() => {
    return {
      speed: 1,
      direction: "forward" as const,
      playOnInit: true,
      stopOnInteraction: false,
      stopOnMouseEnter: true,
      stopOnFocusIn: false,
      ...autoScrollOptions,
    };
  }, [autoScrollOptions]);

  // Initialize embla carousel with auto-scroll plugin
  const [emblaRef, emblaApi] = useEmblaCarousel(emblaOptions, [
    AutoScroll(autoScrollPluginOptions),
  ]);

  // Provide API to parent component if requested
  React.useEffect(() => {
    if (emblaApi && setApi) {
      setApi(emblaApi);
    }
  }, [emblaApi, setApi]);

  // Render slides - duplicate content for seamless looping
  const renderSlides = useMemo(() => {
    if (!Array.isArray(data) || data.length === 0) {
      return null;
    }

    return data.map((item, index) => (
      <div
        key={`slide-${index}`}
        className={cnBase(
          "oui-shrink-0",
          isHorizontal ? "oui-w-auto" : "oui-h-auto",
        )}
        style={{
          minWidth: isHorizontal ? "auto" : "100%",
          minHeight: isHorizontal ? "100%" : "auto",
        }}
      >
        {renderItem(item, index % data.length)}
      </div>
    ));
  }, [data, renderItem, isHorizontal]);

  return (
    <div
      ref={emblaRef}
      className={cnBase(
        "oui-relative oui-overflow-hidden",
        isHorizontal ? "oui-w-full" : "oui-h-full",
        className,
      )}
    >
      <div
        className={cnBase(
          "oui-flex",
          isHorizontal ? "oui-flex-row" : "oui-flex-col",
        )}
      >
        {renderSlides}
      </div>
    </div>
  );
};

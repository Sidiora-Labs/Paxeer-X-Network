"use client";

import React from "react";
import {
  Button,
  ChevronLeftIcon,
  ChevronRightIcon,
  cn,
  useEmblaCarousel,
  type UseEmblaCarouselType,
} from "@paxeer-network/ui";

type CarouselApi = UseEmblaCarouselType[1];
type UseCarouselParameters = Parameters<typeof useEmblaCarousel>;
type CarouselOptions = UseCarouselParameters[0];
type CarouselPlugin = UseCarouselParameters[1];

type CarouselProps = {
  opts?: CarouselOptions;
  plugins?: CarouselPlugin;
  orientation?: "horizontal" | "vertical";
  setApi?: (api: CarouselApi) => void;
  initIndex?: number;
};

type CarouselContextProps = {
  carouselRef: ReturnType<typeof useEmblaCarousel>[0];
  api: ReturnType<typeof useEmblaCarousel>[1];
  scrollPrev: () => void;
  scrollNext: () => void;
  canScrollPrev: boolean;
  canScrollNext: boolean;
  selectedIndex: number;
  scrollSnaps: number[];
} & CarouselProps;

const CarouselContext = React.createContext<CarouselContextProps | null>(null);

export function useCarousel() {
  const context = React.useContext(CarouselContext);

  if (!context) {
    throw new Error("useCarousel must be used within a <Carousel />");
  }

  return context;
}

const Carousel = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement> & CarouselProps
>((originalProps, ref) => {
  const {
    orientation = "horizontal",
    opts,
    setApi,
    plugins,
    className,
    children,
    ...props
  } = originalProps;
  const [carouselRef, api] = useEmblaCarousel(
    { ...opts, axis: orientation === "horizontal" ? "x" : "y" },
    plugins,
  );
  const [canScrollPrev, setCanScrollPrev] = React.useState(false);
  const [canScrollNext, setCanScrollNext] = React.useState(false);
  const [selectedIndex, setSelectedIndex] = React.useState(
    props.initIndex || 0,
  );
  const [scrollSnaps, setScrollSnaps] = React.useState<number[]>([]);

  const onSelect = React.useCallback(
    (api: CarouselApi) => {
      if (!api) {
        return;
      }

      // console.log("selected", api.scrollSnapList());

      if (scrollSnaps.length === 0) {
        setScrollSnaps(api.scrollSnapList());
      }

      setSelectedIndex(api.selectedScrollSnap());

      setCanScrollPrev(api.canScrollPrev());
      setCanScrollNext(api.canScrollNext());
    },
    [scrollSnaps],
  );

  const scrollPrev = React.useCallback(() => {
    api?.scrollPrev();
  }, [api]);

  const scrollNext = React.useCallback(() => {
    api?.scrollNext();
  }, [api]);

  const handleKeyDown = React.useCallback(
    (event: React.KeyboardEvent<HTMLDivElement>) => {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        scrollPrev();
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        scrollNext();
      }
    },
    [scrollPrev, scrollNext],
  );

  React.useEffect(() => {
    if (!api || !setApi) {
      return;
    }

    setApi(api);
  }, [api, setApi]);

  React.useEffect(() => {
    if (!api) {
      return;
    }

    onSelect(api);
    api.on("reInit", onSelect);
    api.on("select", onSelect);

    if (props.initIndex) {
      api.scrollTo(props.initIndex);
    }
    return () => {
      api?.off("select", onSelect);
    };
  }, [api, onSelect]);

  const memoizedValue = React.useMemo<CarouselContextProps>(() => {
    return {
      carouselRef,
      api: api,
      opts,
      orientation:
        orientation || (opts?.axis === "y" ? "vertical" : "horizontal"),
      scrollPrev,
      scrollNext,
      canScrollPrev,
      canScrollNext,
      selectedIndex,
      scrollSnaps,
    };
  }, [
    carouselRef,
    api,
    opts,
    orientation,
    scrollPrev,
    scrollNext,
    canScrollPrev,
    canScrollNext,
    selectedIndex,
    scrollSnaps,
  ]);

  return (
    <CarouselContext.Provider value={memoizedValue}>
      <div
        ref={ref}
        onKeyDownCapture={handleKeyDown}
        className={cn("oui-relative", className)}
        role="region"
        aria-roledescription="carousel"
        {...props}
      >
        {children}
      </div>
    </CarouselContext.Provider>
  );
});

Carousel.displayName = "Carousel";

const CarouselContent = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>((originalProps, ref) => {
  const { className, children, ...props } = originalProps;
  const { carouselRef, orientation } = useCarousel();
  return (
    <div ref={carouselRef} className="oui-overflow-hidden">
      <div
        ref={ref}
        className={cn(
          "oui-flex",
          orientation === "horizontal" ? "oui--ml-4" : "oui--mt-4 oui-flex-col",
          className,
        )}
        {...props}
      >
        {children}
      </div>
    </div>
  );
});

CarouselContent.displayName = "CarouselContent";

const CarouselItem = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>((originalProps, ref) => {
  const { className, children, ...props } = originalProps;
  const { orientation } = useCarousel();
  return (
    <div
      ref={ref}
      role="group"
      aria-roledescription="slide"
      className={cn(
        "oui-min-w-0 oui-shrink-0 oui-grow-0 oui-basis-full",
        orientation === "horizontal" ? "oui-ps-4" : "oui-pt-4",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
});

CarouselItem.displayName = "CarouselItem";

const CarouselPrevious = React.forwardRef<
  HTMLButtonElement,
  React.ComponentProps<typeof Button>
>((originalProps, ref) => {
  const {
    className,
    variant = "contained",
    size = "icon",
    ...props
  } = originalProps;
  const { orientation, scrollPrev, canScrollPrev } = useCarousel();
  return (
    <Button
      ref={ref}
      variant={variant}
      // size={size}
      className={cn(
        "oui-absolute oui-size-8 oui-rounded-full",
        orientation === "horizontal"
          ? "oui--start-12 oui-top-1/2 oui--translate-y-1/2"
          : "oui--top-12 oui-start-1/2 oui--translate-x-1/2 oui-rotate-90",
        className,
      )}
      disabled={!canScrollPrev}
      onClick={scrollPrev}
      {...props}
    >
      <ChevronLeftIcon size={20} />
      <span className="oui-sr-only">Previous slide</span>
    </Button>
  );
});

CarouselPrevious.displayName = "CarouselPrevious";

const CarouselNext = React.forwardRef<
  HTMLButtonElement,
  React.ComponentProps<typeof Button>
>((originalProps, ref) => {
  const {
    className,
    variant = "contained",
    size = "icon",
    ...props
  } = originalProps;
  const { orientation, scrollNext, canScrollNext } = useCarousel();
  return (
    <Button
      ref={ref}
      variant={variant}
      // size={size}
      className={cn(
        "oui-absolute oui-size-8 oui-rounded-full",
        orientation === "horizontal"
          ? "oui--end-12 oui-top-1/2 oui--translate-y-1/2"
          : "oui--bottom-12 oui-start-1/2 oui--translate-x-1/2 oui-rotate-90",
        className,
      )}
      disabled={!canScrollNext}
      onClick={scrollNext}
      {...props}
    >
      <ChevronRightIcon size={20} />
      <span className="oui-sr-only">Next slide</span>
    </Button>
  );
});

CarouselNext.displayName = "CarouselNext";

export type CarouselIdentifierProps = {
  className?: string;
  dotClassName?: string;
  dotActiveClassName?: string;
  onClick?: (index: number) => void;
  // asChild?: boolean;
};

const CarouselIdentifier: React.FC<CarouselIdentifierProps> = (props) => {
  const { scrollSnaps, selectedIndex } = useCarousel();
  return (
    <div className={cn("oui-flex oui-gap-1", props.className)}>
      {scrollSnaps.map((_, index) => {
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

CarouselIdentifier.displayName = "CarouselIdentifier";

export const Dot: React.FC<{
  index: number;
  active: boolean;
  onClick?: (index: number) => void;
  className?: string;
  activeClassName?: string;
}> = ({ index, active, onClick, className, activeClassName }) => {
  const activedClassName = activeClassName || "oui-bg-primary-darken";
  return (
    <button
      onClick={() => onClick?.(index)}
      className={cn(
        "oui-size-2 oui-rounded-full oui-bg-base-contrast-30",
        className,
        active && `active ${activedClassName}`,
      )}
    />
  );
};

export {
  type CarouselApi,
  Carousel,
  CarouselContent,
  CarouselItem,
  CarouselPrevious,
  CarouselNext,
  CarouselIdentifier,
};

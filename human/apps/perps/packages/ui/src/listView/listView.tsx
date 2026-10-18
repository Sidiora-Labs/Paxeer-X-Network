import React, { ForwardedRef, forwardRef, useImperativeHandle } from "react";
import { useMemo, useRef } from "react";
import { cn } from "tailwind-variants";
import { Flex } from "../flex";
import { Spinner } from "../spinner";
import { EmptyDataState } from "../table";
import { useEndReached } from "./useEndReached";

export interface ListViewProps<T, D> {
  dataSource: T[] | null | undefined;
  renderItem: (item: T, index: number, extraData?: D) => React.ReactNode;
  /**
   * Extract a stable key from each item. Must be unique among items and
   * stable across reorders/removals; defaults to the array index, which is
   * only safe for static or append-only lists.
   */
  keyExtractor?: (item: T, index: number) => React.Key;
  className?: string;
  contentClassName?: string;
  isLoading?: boolean;
  loadMore?: () => void;

  style?: React.CSSProperties;

  extraData?: D;
  emptyView?: React.ReactNode;
}

export type ListViewRef = ForwardedRef<{
  scroll: (direction: { x: number; y: number }) => void;
}>;

const ListViewInner = <T, D>(props: ListViewProps<T, D>, ref: ListViewRef) => {
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  const containerRef = useRef<HTMLDivElement | null>(null);

  useEndReached(sentinelRef, () => {
    if (!props.isLoading) {
      props.loadMore?.();
    }
  });

  const emptyDataSouce = useMemo(() => {
    return Array.isArray(props.dataSource) && props.dataSource.length <= 0;
  }, [props.dataSource]);

  const listViewElement = useMemo(() => {
    if (!props.dataSource) {
      return null;
    }

    if (emptyDataSouce) {
      return (
        props.emptyView || (
          <Flex
            direction={"column"}
            height={"100%"}
            itemAlign={"center"}
            justify={"center"}
            mt={3}
          >
            <EmptyDataState />
          </Flex>
        )
      );
    }

    return props.dataSource.map((item, index) => (
      <React.Fragment
        key={props.keyExtractor ? props.keyExtractor(item, index) : index}
      >
        {props.renderItem(item, index, props.extraData)}
      </React.Fragment>
    ));
  }, [
    emptyDataSouce,
    props.dataSource,
    props.extraData,
    props.emptyView,
    props.keyExtractor,
    props.renderItem,
  ]);

  const loadingViewElement = useMemo(() => {
    if ((props.dataSource?.length || 0) === 0) return null;
    if (!props.isLoading) {
      return null;
    }

    return (
      <div className="oui-flex oui-py-2 oui-justify-center oui-items-center">
        <Spinner />
      </div>
    );
  }, [props.isLoading, props.dataSource]);

  useImperativeHandle(ref, () => {
    return {
      scroll: (direction) => {
        containerRef.current?.scroll({
          left: direction.x,
          top: direction.y,
          behavior: "smooth",
        });
      },
    };
  });

  return (
    <div
      style={props.style}
      ref={containerRef}
      className={cn(
        "oui-custom-scrollbar oui-relative oui-min-h-[180px] oui-overflow-auto",
        props.className,
      )({
        twMerge: true,
      })}
    >
      <div
        className={cn(
          "oui-space-y-3 oui-h-full oui-w-full",
          emptyDataSouce &&
            "oui-absolute oui-bottom-0 oui-start-0 oui-end-0 oui-top-0",
          props.contentClassName,
        )({ twMerge: true })}
      >
        {listViewElement}
      </div>
      <div
        ref={sentinelRef}
        className="oui-relative oui-invisible oui-h-[1px] oui-top-[-300px]"
      />
      {loadingViewElement}
    </div>
  );
};

export const ListView = forwardRef(ListViewInner) as <T, D>(
  props: ListViewProps<T, D> & {
    ref?: ForwardedRef<{
      scroll: (direction: { x: number; y: number }) => void;
    }>;
  },
) => JSX.Element;

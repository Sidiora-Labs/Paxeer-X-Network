/* eslint-disable @typescript-eslint/no-explicit-any */
import React, { useCallback, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  TooltipContent,
  TooltipTrigger,
  Text,
  TooltipRoot,
  TooltipArrow,
  cn,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { BasicSymbolInfo } from "../../../types/types";
import { useOrderBookContext } from "../../base/orderBook/orderContext";
import { OrderBookCellType } from "../../base/orderBook/types";
import { DesktopOrderBookCell } from "./cell.desktop";

interface DesktopListBoxProps {
  type: OrderBookCellType;
  data: number[][];
  countQty: number;
}

const calcHintInfo = (item: number[] | null) => {
  if (!item) {
    return { sumQty: 0, sumQtyAmount: 0, avgPrice: 0 };
  }
  const [, , qty, amount] = item;
  const sumQty = !Number.isNaN(qty) ? qty : 0;
  const sumQtyAmount = !Number.isNaN(amount) ? amount : 0;
  return {
    sumQty: sumQty,
    sumQtyAmount: sumQtyAmount,
    avgPrice:
      sumQtyAmount === 0 || sumQty === 0
        ? 0
        : new Decimal(sumQtyAmount).div(sumQty).toNumber(),
  };
};

export const DesktopListBox: React.FC<DesktopListBoxProps> = (props) => {
  const { data, type, countQty } = props;
  const { symbolInfo, depth } = useOrderBookContext();

  const findMaxItem = useCallback(() => {
    if (!data?.length) {
      return null;
    }
    if (type === OrderBookCellType.ASK) {
      const index = data.findIndex((item) => !Number.isNaN(item[0]));
      if (index !== -1) {
        return data[index];
      }
      return null;
    } else {
      const len = data.length;
      for (let index = len - 1; index >= 0; index--) {
        const item = data[index];
        if (!Number.isNaN(item[0])) {
          return item;
        }
      }
      return null;
    }
  }, [data, type]);

  const priceDp = useMemo(() => {
    if (depth?.toString().includes(".")) {
      return depth.toString().split(".")[1].length;
    }
    return 0;
  }, [depth]);

  const maxQty = useMemo(() => {
    return data.reduce((a, b) => Math.max(a, b[1]), 0);
  }, [data]);

  const [hoverIndex, setHoverIndex] = useState<number>(-1);

  return (
    <div className="oui-order-book-list oui-orderBook-list oui-flex oui-flex-col oui-gap-px">
      {data.map((item, index) => {
        return (
          <Tip
            key={index}
            index={index}
            item={item}
            countQty={countQty}
            setHoverIndex={setHoverIndex}
            hoverIndex={hoverIndex}
            type={type}
            maxQty={maxQty}
            priceDp={priceDp}
            symbolInfo={symbolInfo}
            findMaxItem={findMaxItem}
          />
        );
      })}
    </div>
  );
};

const Tip: React.FC<{
  index: number;
  item: any;
  countQty: number;
  setHoverIndex: any;
  type: OrderBookCellType;
  maxQty: number;
  hoverIndex: number;
  priceDp: number;
  // base: any;
  // quote: any;
  // baseDp: number;
  // quoteDp: number;
  findMaxItem: () => number[] | null;
  symbolInfo: BasicSymbolInfo;
}> = (props) => {
  const {
    index,
    item,
    setHoverIndex,
    type,
    maxQty,
    hoverIndex,
    priceDp,
    countQty,
    symbolInfo,
    findMaxItem,
  } = props;

  const { base, quote, base_dp: baseDp, quote_dp: quoteDp } = symbolInfo;
  const { t } = useTranslation();

  const isHover =
    hoverIndex !== -1
      ? type === OrderBookCellType.ASK
        ? index >= hoverIndex
        : index <= hoverIndex
      : false;

  const [open, setOpen] = useState(false);

  const hintInfo = useMemo(() => {
    const info = calcHintInfo(item);
    return info.avgPrice === 0 ? calcHintInfo(findMaxItem()) : info;
  }, [item, findMaxItem]);

  return (
    <TooltipRoot open={open} onOpenChange={setOpen}>
      <TooltipTrigger>
        <DesktopOrderBookCell
          background={""}
          price={item[0]}
          quantity={item[1]}
          accumulated={item[2]}
          count={countQty}
          type={type}
          accumulatedAmount={item[3]}
          maxQty={maxQty}
          isHover={isHover}
          currentHover={hoverIndex === index}
          symbolInfo={symbolInfo}
          base={base}
          quote={quote}
          onMouseEnter={() => {
            setHoverIndex(index);
            setOpen(true);
          }}
          onMouseLeave={() => {
            setHoverIndex(-1);
            setOpen(false);
          }}
        />
      </TooltipTrigger>
      <TooltipContent
        className={cn(
          "oui-rounded-base oui-flex oui-w-full oui-max-w-[400px] oui-flex-col oui-gap-2 oui-bg-base-6 oui-p-3 oui-text-2xs oui-shadow-md",
          // type === OrderBookCellType.ASK ? `oui-mb-${30}` : "oui-mt-0"
        )}
        align={type === OrderBookCellType.ASK ? "end" : "start"}
        alignOffset={-25.4}
        side="left"
        sideOffset={2}
        onPointerEnter={(e) => e.preventDefault()}
      >
        <Row
          title={`${t("common.avgPrice")}≈`}
          content={hintInfo.avgPrice}
          // contentDp={priceDp}
          contentDp={quoteDp}
        />
        <Row
          title={`${t("trading.orderBook.sum")} (${base})`}
          content={hintInfo.sumQty}
          contentDp={baseDp}
        />
        <Row
          title={`${t("trading.orderBook.sum")} (${quote})`}
          content={hintInfo.sumQtyAmount}
          contentDp={quoteDp}
        />
        <TooltipArrow
          className="oui-fill-base-6"
          style={{
            transform:
              type === OrderBookCellType.ASK
                ? "translateX(80%)"
                : "translateX(-80%)",
          }}
        />
      </TooltipContent>
    </TooltipRoot>
  );
};

const Row: React.FC<{ title: string; content: number; contentDp: number }> = (
  props,
) => {
  const { title, content, contentDp } = props;
  return (
    <div className="oui-flex oui-flex-row oui-justify-between oui-gap-4">
      <div className="oui-select-none oui-text-base-contrast-36">{title}</div>
      <div className="oui-text-end">
        <Text.numeral dp={contentDp}>{content}</Text.numeral>
      </div>
    </div>
  );
};

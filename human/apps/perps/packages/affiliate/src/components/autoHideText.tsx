import { FC, useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@paxeer-network/ui";

export const AutoHideText: FC<{
  text: string;
  className?: string;
  visibleCount?: number;
}> = (props) => {
  const { text, visibleCount } = props;
  const containerRef = useRef<HTMLDivElement | null>(null);
  const textRef = useRef<HTMLDivElement>(null);
  const lastContainerWidth = useRef<number | undefined>(undefined);

  function getTextWidth(ref: HTMLDivElement, text: string): number | undefined {
    const canvas = document.createElement("canvas");
    const context = canvas.getContext("2d");
    const element = ref;
    if (context && element) {
      let fontStyle = getComputedStyle(element).font;
      if (fontStyle.length === 0) {
        fontStyle = [
          getComputedStyle(element).fontWeight,
          getComputedStyle(element).fontSize,
          getComputedStyle(element).fontFamily,
        ].join(" ");
      }
      context.font = fontStyle;
      context.letterSpacing = getComputedStyle(element).letterSpacing;

      const width = context.measureText(text).width;
      return width;
    }
    return undefined;
  }

  const [visibleChars, setVisibleChars] = useState(0);

  useEffect(() => {
    const calculateVisibleChars = () => {
      if (props.visibleCount !== undefined) return;
      if (containerRef.current && textRef.current) {
        const containerWidth = containerRef.current.clientWidth;

        if (containerWidth == lastContainerWidth.current) return;
        lastContainerWidth.current = containerWidth;

        const textElement = textRef.current;

        const textContent = text;

        const pWidth = Math.ceil(getTextWidth(textElement, "...") || 0);

        const maxDisplayW = containerWidth - pWidth;
        let count = 0;
        while (count < text.length) {
          const contentW =
            getTextWidth(textElement, subText(text, count++)) || maxDisplayW;

          if (contentW <= maxDisplayW) {
            break;
          }
        }
        setVisibleChars(textContent.length - count);
      }
    };

    lastContainerWidth.current = undefined;
    calculateVisibleChars();

    window.addEventListener("resize", calculateVisibleChars);
    return () => {
      lastContainerWidth.current = undefined;
      window.removeEventListener("resize", calculateVisibleChars);
    };
  }, [text, textRef]);

  const truncatedText = useMemo(() => {
    if (visibleChars + 1 >= text.length) {
      return text;
    }

    const maxCharacters = visibleCount || visibleChars;

    const startText = text.slice(0, Math.ceil(maxCharacters / 2));
    const endText = text.slice(
      text.length - Math.floor(maxCharacters / 2),
      text.length,
    );

    return `${startText}...${endText}`;
  }, [text, visibleCount, visibleChars]);

  return (
    <div
      ref={containerRef}
      className={cn("oui-relative oui-w-full oui-text-end", props.className)}
    >
      <div
        className={cn(
          "oui-hidden oui-whitespace-nowrap oui-absolute oui-top-0 oui-bottom-0 oui-end-0 oui-start-0",
        )}
      >
        <span ref={textRef}>{text}</span>
      </div>
      <span>{truncatedText}</span>
    </div>
  );
};

const subText = (text: string, count: number) => {
  const start = Math.floor(text.length / 2 - (count > 0 ? count / 2 : 1));
  const first = text.slice(0, start);
  const end = text.slice(start + count);
  return first + end;
};

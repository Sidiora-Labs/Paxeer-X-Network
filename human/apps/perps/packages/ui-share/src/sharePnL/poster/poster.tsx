import { forwardRef, useImperativeHandle } from "react";
import { type DrawOptions, usePoster } from "@paxeer-network/hooks";

export type PosterProps = {
  width: number;
  height: number;
  className?: string;
  ratio?: number;
  data: DrawOptions;
  style?: React.CSSProperties;
};

export type PosterRef = {
  download: (filename: string, type?: string, encoderOptions?: number) => void;
  toDataURL: (type?: string, encoderOptions?: number) => string;
  toBlob: (type?: string, encoderOptions?: number) => Promise<Blob | null>;
  copy: () => Promise<void>;
};

export const Poster = forwardRef<PosterRef, PosterProps>((props, parentRef) => {
  const { width, height, className, data, style } = props;

  const { ref, download, toDataURL, copy, toBlob } = usePoster(data, {
    ratio: props.ratio,
  });

  useImperativeHandle(parentRef, () => ({
    download,
    toDataURL,
    toBlob,
    copy,
  }));

  return (
    <canvas
      ref={ref}
      width={width}
      height={height}
      dir="ltr"
      className={className}
      style={style}
    />
  );
});

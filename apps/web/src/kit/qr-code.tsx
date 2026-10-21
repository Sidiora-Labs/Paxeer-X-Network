"use client";

import QRCode from "qrcode";
import { useMemo } from "react";

export function AddressQrCode({ value, label }: Readonly<{ value: string; label: string }>) {
  const code = useMemo(() => QRCode.create(value, { errorCorrectionLevel: "M" }), [value]);
  const quiet = 4;
  const size = code.modules.size;
  const viewBox = size + quiet * 2;
  const cells = [];
  for (let row = 0; row < size; row += 1) {
    for (let column = 0; column < size; column += 1) {
      if (code.modules.get(row, column)) {
        cells.push(<rect key={`${String(row)}-${String(column)}`} x={column + quiet} y={row + quiet} width="1" height="1" />);
      }
    }
  }
  return (
    <svg
      aria-label={label}
      role="img"
      viewBox={`0 0 ${String(viewBox)} ${String(viewBox)}`}
      className="size-44 max-w-full bg-white text-black"
      shapeRendering="crispEdges"
    >
      <rect width={viewBox} height={viewBox} fill="white" />
      <g fill="currentColor">{cells}</g>
    </svg>
  );
}

//@ts-ignore
// import QRious from "./qrious";
import { qrcode } from "@akamfoad/qr";

export const qrPaint = (
  ctx: CanvasRenderingContext2D,
  options: {
    size: number;
    padding: number;
    left: number;
    top: number;
    data: string;
  }
) => {
  const { size, left, top, data, padding } = options;

  const qr = qrcode(data);
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const ctx2 = canvas.getContext("2d");

  // const qr = new QRious({
  //   value: data,
  //   size: 200,
  //   padding: 5,
  //   level: "L",
  // });

  const cells = qr.modules;

  if (!cells) {
    return;
  }

  const tileW = size / cells.length;
  const tileH = size / cells.length;

  for (let r = 0; r < cells.length; ++r) {
    const row = cells[r];
    for (let c = 0; c < row.length; ++c) {
      ctx2!.fillStyle = row[c] ? "#000" : "#fff";
      const w = Math.ceil((c + 1) * tileW) - Math.floor(c * tileW);
      const h = Math.ceil((r + 1) * tileH) - Math.floor(r * tileH);
      ctx2!.fillRect(Math.round(c * tileW), Math.round(r * tileH), w, h);
    }
  }

  ctx.save();
  ctx.fillStyle = "#fff";

  ctx.fillRect(
    left - padding,
    top - padding,
    size + padding * 2,
    size + padding * 2
  );
  ctx.restore();

  ctx.drawImage(canvas, left, top, size, size);
};

import { BackgroundPaint } from "./backgroundPaint";
import { DrawOptions } from "./basePaint";
import { DataPaint } from "./dataPaint";
export class PosterPainter {
  private ctx: CanvasRenderingContext2D;
  width: number = 0;
  height: number = 0;
  ratio: number;
  // resourceManager: Resource;
  constructor(
    private canvas: HTMLCanvasElement,
    options?: {
      ratio: number;
    }
  ) {
    // console.log("PosterPainter constructor");

    this.ctx = this.canvas.getContext("2d")!;

    this.width = this.canvas.width;
    this.height = this.canvas.height;

    // this.ratio =
    //   options?.ratio ||
    //   (typeof window !== "undefined"
    //     ? Math.ceil(window.devicePixelRatio)
    //     : 1) ||
    //   1;

    this.ratio = Math.max(
      options?.ratio || 1,
      typeof window !== "undefined" ? Math.ceil(window.devicePixelRatio) : 1
    );

    // render to high resolution
    this.canvas.width = this.width * this.ratio;
    this.canvas.height = this.height * this.ratio;
    this.canvas.style.width = this.width + "px";
    this.canvas.style.height = this.height + "px";

    //
  }

  draw(options: DrawOptions) {
    requestAnimationFrame(() => this._draw.bind(this)(options));
  }

  async _draw(options: DrawOptions) {
    if (this.ctx === null) return;
    // console.log("============ DRAW ============");
    // this.ctx.font = options.fontFamily!;
    // this.ctx.clearRect(0, 0, this.width * this.ratio, this.height * this.ratio);
    // start draw
    // background
    await new BackgroundPaint(this.ctx, this).draw(options);
    // data paint
    await new DataPaint(this.ctx, this).draw(options);
  }
}

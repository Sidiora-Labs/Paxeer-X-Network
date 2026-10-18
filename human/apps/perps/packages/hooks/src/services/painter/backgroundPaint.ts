import { BasePaint, type DrawOptions } from "./basePaint";

export class BackgroundPaint extends BasePaint {
  private img: HTMLImageElement | null = null;

  async draw(options: DrawOptions) {
    if (
      typeof options.backgroundImg !== "undefined" &&
      options.backgroundImg !== ""
    ) {
      return this._drawImage(options);
    } else if (typeof options.backgroundColor !== "undefined") {
      return this._drawColor(options);
    }
  }

  _drawColor(options: DrawOptions) {
    console.log("graw background color", options);
    this.ctx.fillStyle = options.backgroundColor || "black";
    this.ctx.fillRect(
      0,
      0,
      this.painter.width * this.painter.ratio,
      this.painter.height * this.painter.ratio,
    );
  }

  async _drawImage(options: DrawOptions) {
    const width = this.painter.width * this.painter.ratio;
    const height = this.painter.height * this.painter.ratio;

    return this.loadImg(options.backgroundImg!).then((img) => {
      this.img = img;
      this.ctx.save();
      if (options.direction === "rtl") {
        this.ctx.translate(width, 0);
        this.ctx.scale(-1, 1);
      }
      this.ctx.drawImage(this.img!, 0, 0, width, height);
      this.ctx.restore();
    });
  }

  private loadImg(url: string): Promise<HTMLImageElement> {
    return new Promise((resolve, reject) => {
      const img = new Image();
      img.crossOrigin = "anonymous";
      img.onload = () => {
        resolve(img);
      };
      img.onerror = (e) => {
        reject(e);
      };
      img.src = url;
    });
  }
}

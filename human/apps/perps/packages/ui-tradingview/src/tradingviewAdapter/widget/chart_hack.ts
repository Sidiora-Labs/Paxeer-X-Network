import { waitForElm } from "./util";

export class ChartHack {
  private iframeDocument: any;

  constructor({ iframeDocument }: { iframeDocument: any }) {
    this.iframeDocument = iframeDocument;
  }

  public defaultHack() {
    this.showFavoriteStarByDefault();
  }

  private showFavoriteStarByDefault() {
    waitForElm(this.iframeDocument, ".dropdown-2R6OKuTS").then(() => {
      const items = this.iframeDocument.querySelectorAll(
        ".toolbox-2IihgTnv.showOnHover-2IihgTnv"
      );
      items.forEach((item: HTMLElement) => {
        item.style.opacity = "1";
      });
    });
  }
}

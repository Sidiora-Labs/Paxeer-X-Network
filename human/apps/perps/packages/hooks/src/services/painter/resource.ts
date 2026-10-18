export class Resource {
  sourceMap: Map<string, HTMLImageElement> = new Map();

  constructor() {
    console.log("Resource constructor");
  }

  has(source: string) {
    return this.sourceMap.has(source);
  }

  load(source: string) {
    console.log("Resource load: ", source);
    const img = new Image();
    img.src = source;
    this.sourceMap.set(source, img);
  }
}

const waitForElm = (
  iframeDocument: HTMLElement,
  selector: string
): Promise<Element | null> => {
  return new Promise((resolve) => {
    const initialIframe = iframeDocument.querySelector(selector);
    if (initialIframe) {
      resolve(initialIframe);
    }

    const observer = new MutationObserver(() => {
      const iframe = iframeDocument.querySelector(selector);
      if (iframe) {
        resolve(iframe);
        observer.disconnect();
      }
    });

    observer.observe(iframeDocument, {
      childList: true,
      subtree: true,
    });
  });
};


export { waitForElm };

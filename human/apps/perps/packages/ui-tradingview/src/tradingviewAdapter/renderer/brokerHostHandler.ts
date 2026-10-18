import {
  IBrokerConnectionAdapterHost,
  IChartingLibraryWidget,
} from "../charting_library";

function preventDefaultRenderHack(host: IBrokerConnectionAdapterHost) {
  (host as any).setBrokerConnectionAdapter = function (adapter: any) {
    const delegate = {
      subscribe: () => {},
      unsubscribe: () => {},
      unsubscribeAll: () => {},
    };

    Object.defineProperty(adapter, "_ordersCache", {
      get: function () {
        return {
          start: () => {},
          stop: () => {},
          update: () => {},
          partialUpdate: () => {},
          fullUpdate: () => {},
          getObjects: async () => [],
          updateDelegate: delegate,
          partialUpdateDelegate: delegate,
        };
      },
      set: () => {},
    });
    adapter._waitForOrderModification = async () => true;
    this._adapter = adapter;
  };
}

function forceSilentOrdersPlacement(
  instance: IChartingLibraryWidget,
  host: IBrokerConnectionAdapterHost
) {
  instance.onChartReady(() => {
    // Apply default teamplate makes the silentOrdersPlacement disabled, need to set it back
    host.silentOrdersPlacement().subscribe((val: any) => {
      if (!val) {
        host.silentOrdersPlacement().setValue(true);
        if (instance) {
          (instance as any)._iFrame.contentDocument.querySelector(
            ".wrapper-3X2QgaDd"
          ).className = "wrapper-3X2QgaDd highButtons-3X2QgaDd";
        }

        // The block can only be executed while "Apply default teamplate".
        // We don't wnat to show btn buy defualt, so close it.
        host.sellBuyButtonsVisibility()?.setValue(false);
      }
    });
  });
}

export default function brokerHostHandler(
  instance: IChartingLibraryWidget,
  host: IBrokerConnectionAdapterHost
) {
  preventDefaultRenderHack(host);
  forceSilentOrdersPlacement(instance, host);
}

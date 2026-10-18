import {
  createElement,
  type ComponentType,
  type FC,
  type PropsWithChildren,
  useEffect,
} from "react";
import {
  type ExtensionPosition,
  PaxeerNetworkPluginRegistry,
  positionToPath,
} from "../plugin";

const COMPONENTS_PLUGIN_ID = "paxeer-network-components-provider-overrides";

const ComponentsProvider: FC<
  PropsWithChildren<{
    components?: { [position in ExtensionPosition]: ComponentType };
  }>
> = (props) => {
  useEffect(() => {
    if (props.components && Object.keys(props.components).length) {
      const interceptors = Object.entries(props.components).map(
        ([position, Element]) => ({
          target: positionToPath(position),
          component: (_Original: ComponentType<any>, slotProps: any) =>
            createElement(Element as ComponentType<any>, slotProps),
        }),
      );

      PaxeerNetworkPluginRegistry.register({
        id: COMPONENTS_PLUGIN_ID,
        name: "ComponentsProvider",
        interceptors,
      });
    }
  }, [props.components]);

  return props.children;
};

export { ComponentsProvider };

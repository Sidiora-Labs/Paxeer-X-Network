import { type ComponentType } from "react";
import { registerInjectableTarget } from "./injectableTargetRegistry";
import { useInjectedComponent } from "./useInjectedComponent";

/**
 * HOC: wraps a component to make it interceptable by plugins.
 * Registers the target path in a static registry so it appears in interceptor target lists.
 */
export function injectable<P extends object>(
  Component: ComponentType<P>,
  name: string,
): ComponentType<P> {
  registerInjectableTarget(name);
  const InjectableComponent = (props: P) => {
    const Injected = useInjectedComponent(name, Component);
    return <Injected {...props} />;
  };
  InjectableComponent.displayName = `injectable(${name})`;
  return InjectableComponent;
}

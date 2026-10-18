import { getGlobalObject } from "../utils";
import Container from "./container";

//https://github.com/kl4n4/typescript-simple-di
export class SimpleDI {
  private static KEY = "__PAXEER_NETWORK_CONTAINER__";
  private static container: Container =
    (getGlobalObject() as any)[SimpleDI.KEY] || null;

  private static getContainer(): Container {
    if (!SimpleDI.container) {
      (getGlobalObject() as any)[SimpleDI.KEY] = SimpleDI.container =
        new Container();
    }
    return SimpleDI.container;
  }

  static register(...serviceClasses: any[]): void {
    SimpleDI.getContainer().register(...serviceClasses);
  }

  static registerByName(name: string, serviceClass: any): void {
    SimpleDI.getContainer().registerByName(name, serviceClass);
  }

  // static addInjectProperty(target: any, propertyKey: string, serviceName: string = propertyKey): void {
  //     SimpleDI.getContainer().addInjectProperty(target, propertyKey, serviceName);
  // }

  static get<T = any>(name: string): T {
    return SimpleDI.getContainer().get<T>(name);
  }

  static getOr<T = any>(name: string, instance: T): T {
    const s = SimpleDI.getContainer().get<T>(name);
    if (!s) {
      SimpleDI.registerByName(name, instance);
    }
    return instance;
  }

  // static getByType<T>(c: new (...args: any[]) => T): T {
  //     return SimpleDI.getContainer().getByType(c);
  // }

  static getAll(): { [name: string]: any } {
    return SimpleDI.getContainer().getAll();
  }

  private constructor() {}
}

export default SimpleDI;

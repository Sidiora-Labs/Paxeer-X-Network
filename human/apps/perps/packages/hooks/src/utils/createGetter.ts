import { isNil } from "ramda";
import { API } from "@paxeer-network/types";

/** Defines the depth of object access (1 or 2 levels) */
type ObjectDepth = 1 | 2;

/** Type for the input data object */
type Data<T> = Record<string, T> | null | undefined;

/** Interface for checking if a value is nil */
type Nil = { isNil: boolean };

/**
 * Creates a getter proxy for accessing nested object properties with optional default values.
 * This overload handles two-level deep object access.
 *
 * @typeParam T - The type of the nested object
 * @typeParam D - The depth of object access (default is 2)
 * @typeParam K - The keys of type T
 *
 * @param data - The source data object
 * @param depth - The depth of object access (1 or 2)
 *
 * @returns A proxy object that provides safe access to nested properties
 *
 * @example
 * ```typescript
 * const data = { user: { name: 'John', age: 30 } };
 * const getter = createGetter(data);
 * const name = getter.user('name'); // Returns 'John'
 * const age = getter.user('age', 0); // Returns 30, with 0 as default
 * ```
 */
export function createGetter<
  T extends Record<string, any>,
  D extends ObjectDepth | undefined = 2,
  K extends keyof T = keyof T,
>(
  data: Data<T>,
  depth?: D,
): Record<string, <Key extends K>(key: Key, defaultValue?: T[Key]) => T[Key]> &
  Record<string, () => T> &
  Nil;

/**
 * Creates a getter proxy for accessing object properties with optional default values.
 * This overload handles single-level object access.
 *
 * @typeParam T - The type of the object
 * @typeParam D - The depth of object access (must be 1)
 * @typeParam K - The keys of type T
 *
 * @param data - The source data object
 * @param depth - Must be 1 for this overload
 *
 * @returns A proxy object that provides safe access to properties
 *
 * @example
 * ```typescript
 * const data = { name: 'John', age: 30 };
 * const getter = createGetter(data, 1);
 * const name = getter.name(); // Returns 'John'
 * const age = getter.age(0); // Returns 30, with 0 as default
 * ```
 */
export function createGetter<
  T extends Record<string, any>,
  D extends ObjectDepth = 1,
  K extends keyof T = keyof T,
>(
  data: T,
  depth?: D,
): Record<K, <Key extends K>(defaultValue?: T[Key]) => T[Key]> & Nil;

/**
 * Implementation of the createGetter function.
 * Creates a proxy that provides safe access to object properties with support for default values.
 *
 * @param data - The source data object
 * @param depth - The depth of object access (1 or 2)
 *
 * @returns A proxy object that provides safe access to properties
 */
export function createGetter(data: any, depth: ObjectDepth = 2) {
  const getValue = (value: any, defaultValue?: any) => {
    if (defaultValue === undefined) {
      return value;
    }

    return value ?? defaultValue;
  };

  return new Proxy(data || {}, {
    get(target: any, property: string, receiver): any {
      if (property === "isNil") {
        return isNil(data);
      }
      if (depth === 1) {
        return (defaultValue: any) => {
          if (!target) return defaultValue;
          return getValue(target[property], defaultValue);
        };
      }
      return (key?: string, defaultValue?: any) => {
        if (key) {
          return getValue(target[property]?.[key], defaultValue);
        } else {
          return getValue(target[property], defaultValue);
        }
      };
    },
  });
}

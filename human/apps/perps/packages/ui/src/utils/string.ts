/**
 * capitalize the first letter
 * @param str
 * @returns string
 */
export const capitalizeFirstLetter = (str: string, fallback?: string): undefined | string => {
  if (typeof str === 'undefined' || str === null) return fallback ?? undefined;
  return str.charAt(0).toUpperCase() + str.slice(1);
}
  

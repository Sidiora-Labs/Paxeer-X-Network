export function capitalizeString(str: string): string {
  // Convert the string to lowercase
  const lowercaseStr: string = str.toLowerCase();
  // Capitalize the first letter
  const capitalizedStr: string =
    lowercaseStr.charAt(0).toUpperCase() + lowercaseStr.slice(1);
  return capitalizedStr;
}

export function camelCaseToUnderscoreCase(str: string): string {
  return str.replace(/([a-z])([A-Z])/g, "$1_$2").toLowerCase();
}

export const findLongestCommonSubString = (str1: string, str2: string) => {
  // let index = 0;

  for (let index = 0; index < str1.length; index++) {
    const ele1 = str1.at(index);
    const ele2 = str2.at(index);
    if (ele1 === ele2) {
      continue;
    }

    return index;
  }

  return -1;
};

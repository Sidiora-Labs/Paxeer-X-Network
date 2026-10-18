export function isSameAddress(address1: string, address2: string) {
  return address1.toLowerCase() === address2.toLowerCase();
}

export function getCurrentAddressRowKey(address: string) {
  return `current-address-${address?.toLowerCase()}`;
}

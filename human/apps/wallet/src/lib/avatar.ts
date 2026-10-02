const AVATAR_COUNT = 8;

export function getAvatarPath(address: string): string {
  if (!address) return '/wallet/avatar-1.svg';
  const lastByte = parseInt(address.slice(-2), 16) || 0;
  const index = (lastByte % AVATAR_COUNT) + 1;
  return `/wallet/avatar-${index}.svg`;
}

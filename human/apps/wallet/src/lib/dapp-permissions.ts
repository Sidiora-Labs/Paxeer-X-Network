import {
  dappPermissionsRepository,
  type DappPermissionRecord,
} from '@/platform/storage/repositories';

export const DAPP_PERMISSIONS_CHANGED_EVENT = 'paxport:dapp-permissions-changed';

function announce(): void {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(DAPP_PERMISSIONS_CHANGED_EVENT));
  }
}

export function permissionForOrigin(origin: string): DappPermissionRecord | null {
  try {
    return dappPermissionsRepository.read()[origin] ?? null;
  } catch {
    return null;
  }
}

export function hasDappPermission(
  origin: string,
  address: string,
  chainId: number,
): boolean {
  const permission = permissionForOrigin(origin);
  return Boolean(
    permission &&
    permission.address.toLowerCase() === address.toLowerCase() &&
    permission.chainId === chainId,
  );
}

export function grantDappPermission(
  origin: string,
  address: string,
  chainId: number,
): DappPermissionRecord {
  const now = Date.now();
  const permission: DappPermissionRecord = {
    origin,
    address,
    chainId,
    methods: ['eth_accounts'],
    createdAt: now,
    lastUsedAt: now,
  };
  dappPermissionsRepository.update((current) => ({
    ...current,
    [origin]: permission,
  }));
  announce();
  return permission;
}

export function recordDappMethod(origin: string, method: string): void {
  dappPermissionsRepository.update((current) => {
    const permission = current[origin];
    if (!permission) return current;
    return {
      ...current,
      [origin]: {
        ...permission,
        methods: [...new Set([...permission.methods, method])].slice(-32),
        lastUsedAt: Date.now(),
      },
    };
  });
  announce();
}

export function revokeDappPermission(origin: string): void {
  dappPermissionsRepository.update((current) => {
    const next = { ...current };
    delete next[origin];
    return next;
  });
  announce();
}

export function revokeAllDappPermissions(): void {
  dappPermissionsRepository.write({});
  announce();
}

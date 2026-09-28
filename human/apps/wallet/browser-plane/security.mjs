import dns from 'node:dns/promises';
import net from 'node:net';

const MAX_URL_LENGTH = 2_048;

function isPrivateIpv4(address) {
  const parts = address.split('.').map(Number);
  if (parts.length !== 4 || parts.some(part => !Number.isInteger(part) || part < 0 || part > 255)) {
    return true;
  }
  const [a, b] = parts;
  return (
    a === 0 ||
    a === 10 ||
    a === 127 ||
    (a === 100 && b >= 64 && b <= 127) ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 0) ||
    (a === 192 && b === 168) ||
    (a === 198 && (b === 18 || b === 19)) ||
    (a === 198 && b === 51 && parts[2] === 100) ||
    (a === 203 && b === 0 && parts[2] === 113) ||
    a >= 224
  );
}

function isPrivateIpv6(address) {
  const normalized = address.toLowerCase().split('%')[0];
  if (
    normalized === '::' ||
    normalized === '::1' ||
    normalized.startsWith('fc') ||
    normalized.startsWith('fd') ||
    /^fe[89abcdef]/.test(normalized) ||
    normalized.startsWith('ff') ||
    normalized.startsWith('2001:db8:')
  ) {
    return true;
  }
  const mapped = normalized.match(/::ffff:(\d+\.\d+\.\d+\.\d+)$/);
  if (mapped) return isPrivateIpv4(mapped[1]);
  const mappedHex = normalized.match(/::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/);
  if (mappedHex) {
    const high = Number.parseInt(mappedHex[1], 16);
    const low = Number.parseInt(mappedHex[2], 16);
    return isPrivateIpv4(
      `${high >> 8}.${high & 255}.${low >> 8}.${low & 255}`,
    );
  }
  return false;
}

export function isPrivateAddress(address) {
  const family = net.isIP(address);
  if (family === 4) return isPrivateIpv4(address);
  if (family === 6) return isPrivateIpv6(address);
  return true;
}

function configuredPrivateHosts() {
  return new Set(
    (process.env.BROWSER_PLANE_ALLOW_PRIVATE_HOSTS ?? '')
      .split(',')
      .map(value => value.trim().toLowerCase())
      .filter(Boolean),
  );
}

export async function normalizePublicHttpsUrl(input) {
  if (typeof input !== 'string' || input.length < 1 || input.length > MAX_URL_LENGTH) {
    throw new Error('URL must contain between 1 and 2048 characters.');
  }

  let url;
  try {
    url = new URL(input);
  } catch {
    throw new Error('Enter a valid HTTPS URL.');
  }

  if (url.protocol !== 'https:') {
    throw new Error('Only HTTPS websites can be opened.');
  }
  if (url.username || url.password) {
    throw new Error('URLs containing credentials are not allowed.');
  }

  const hostname = url.hostname.toLowerCase().replace(/^\[|\]$/g, '');
  if (
    hostname === 'localhost' ||
    hostname.endsWith('.localhost') ||
    hostname.endsWith('.local') ||
    hostname.endsWith('.internal')
  ) {
    throw new Error('Private network destinations are not allowed.');
  }

  const allowedPrivateHosts = configuredPrivateHosts();
  if (!allowedPrivateHosts.has(hostname)) {
    let addresses;
    try {
      addresses = net.isIP(hostname)
        ? [{ address: hostname }]
        : await dns.lookup(hostname, { all: true, verbatim: true });
    } catch {
      throw new Error('The website hostname could not be resolved.');
    }
    if (addresses.length === 0 || addresses.some(entry => isPrivateAddress(entry.address))) {
      throw new Error('Private network destinations are not allowed.');
    }
  }

  url.hash = '';
  return url;
}

export async function requestUrlAllowed(input) {
  if (input.startsWith('blob:') || input.startsWith('data:')) return true;
  try {
    await normalizePublicHttpsUrl(input);
    return true;
  } catch {
    return false;
  }
}

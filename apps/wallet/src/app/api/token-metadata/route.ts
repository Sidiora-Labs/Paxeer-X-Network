import { NextRequest, NextResponse } from 'next/server'
import { getObjectString, listKeys } from '@/lib/railwayS3'
import { publicError } from '@/server/http'

export interface TokenMetadata {
  poolAddress: string
  iconUrl?: string
  website?: string
  twitter?: string
  telegram?: string
  github?: string
  discord?: string
  description?: string
  createdAt: number
  updatedAt: number
}

const METADATA_PREFIX = 'launchpad/metadata/'

const ALL_METADATA_TTL_MS = 30_000
let allMetadataCache:
  | { at: number; value: Record<string, TokenMetadata> }
  | null = null
let allMetadataInFlight: Promise<Record<string, TokenMetadata>> | null = null

function normalizeAddress(addr: string) {
  return addr.trim().toLowerCase()
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === 'object' && input !== null && !Array.isArray(input)
}

function optionalUrl(input: unknown): string | undefined {
  if (input === undefined) return undefined
  if (typeof input !== 'string' || input.length > 512) {
    throw new TypeError('Token metadata URL is invalid')
  }
  const value = new URL(input)
  if (value.protocol !== 'https:') {
    throw new TypeError('Token metadata URL is invalid')
  }
  return value.toString()
}

function timestamp(input: unknown): number {
  if (
    typeof input !== 'number' ||
    !Number.isSafeInteger(input) ||
    input < 0
  ) {
    throw new TypeError('Token metadata timestamp is invalid')
  }
  return input
}

function parseTokenMetadata(input: unknown): TokenMetadata {
  if (!isRecord(input) || typeof input.poolAddress !== 'string') {
    throw new TypeError('Token metadata is invalid')
  }
  const poolAddress = normalizeAddress(input.poolAddress)
  if (!/^0x[0-9a-f]{40}$/.test(poolAddress)) {
    throw new TypeError('Token metadata address is invalid')
  }
  if (
    input.description !== undefined &&
    (typeof input.description !== 'string' || input.description.length > 2_000)
  ) {
    throw new TypeError('Token metadata description is invalid')
  }
  return {
    poolAddress,
    iconUrl: optionalUrl(input.iconUrl),
    website: optionalUrl(input.website),
    twitter: optionalUrl(input.twitter),
    telegram: optionalUrl(input.telegram),
    github: optionalUrl(input.github),
    discord: optionalUrl(input.discord),
    description: input.description as string | undefined,
    createdAt: timestamp(input.createdAt),
    updatedAt: timestamp(input.updatedAt),
  }
}

function metadataPathname(poolAddress: string) {
  return `${METADATA_PREFIX}${normalizeAddress(poolAddress)}.json`
}

async function readSingleMetadata(poolAddress: string): Promise<TokenMetadata | null> {
  const pathname = metadataPathname(poolAddress)
  const raw = await getObjectString(pathname, 65_536)
  if (!raw) return null
  return parseTokenMetadata(JSON.parse(raw) as unknown)
}

async function readAllMetadata(): Promise<Record<string, TokenMetadata>> {
  const out: Record<string, TokenMetadata> = {}

  const keys = await listKeys(METADATA_PREFIX, 1_000)

  const BATCH = 25
  for (let i = 0; i < keys.length; i += BATCH) {
    const batch = keys.slice(i, i + BATCH)
    const raws = await Promise.all(batch.map((k) => getObjectString(k, 65_536)))
    for (const raw of raws) {
      if (!raw) continue
      const data = parseTokenMetadata(JSON.parse(raw) as unknown)
      out[data.poolAddress] = data
    }
  }

  return out
}

async function readAllMetadataCached(): Promise<Record<string, TokenMetadata>> {
  const now = Date.now()
  if (allMetadataCache && now - allMetadataCache.at < ALL_METADATA_TTL_MS) {
    return allMetadataCache.value
  }

  if (allMetadataInFlight) return allMetadataInFlight

  allMetadataInFlight = (async () => {
    try {
      const value = await readAllMetadata()
      allMetadataCache = { at: Date.now(), value }
      return value
    } finally {
      allMetadataInFlight = null
    }
  })()

  return allMetadataInFlight
}

export async function GET(request: NextRequest) {
  const { searchParams } = request.nextUrl
  if ([...searchParams.keys()].some((key) => key !== 'poolAddress')) {
    return publicError(400, 'QUERY_INVALID', 'Query is invalid')
  }
  const poolAddress = searchParams.get('poolAddress')

  try {
    if (poolAddress) {
      const normalized = normalizeAddress(poolAddress)
      if (!/^0x[0-9a-f]{40}$/.test(normalized)) {
        return publicError(400, 'ADDRESS_INVALID', 'Token address is invalid')
      }
      const data = await readSingleMetadata(normalized)
      if (!data) {
        return publicError(
          404,
          'TOKEN_METADATA_NOT_FOUND',
          'Token metadata was not found',
        )
      }
      if (data.poolAddress !== normalized) {
        return publicError(
          502,
          'TOKEN_METADATA_MISMATCH',
          'Token metadata is inconsistent',
        )
      }
      return NextResponse.json(data, {
        headers: { 'Cache-Control': 'public, max-age=60' },
      })
    }

    const metadata = await readAllMetadataCached()
    return NextResponse.json(metadata, {
      headers: {
        'Cache-Control': 'public, max-age=15, stale-while-revalidate=60',
      },
    })
  } catch {
    return publicError(
      502,
      'TOKEN_METADATA_UNAVAILABLE',
      'Token metadata is temporarily unavailable',
    )
  }
}

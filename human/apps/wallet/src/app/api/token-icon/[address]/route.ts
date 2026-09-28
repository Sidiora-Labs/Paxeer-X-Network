import { NextRequest, NextResponse } from 'next/server'
import { getObjectBytes } from '@/lib/railwayS3'
import { publicError } from '@/server/http'

const IMAGE_TYPES = new Set([
  'image/gif',
  'image/jpeg',
  'image/png',
  'image/webp',
])

export async function GET(
  _request: NextRequest,
  { params }: { params: Promise<{ address: string }> }
) {
  const resolvedParams = await params
  const address = resolvedParams.address?.toLowerCase()
  if (!address || !/^0x[0-9a-f]{40}$/.test(address)) {
    return publicError(400, 'ADDRESS_INVALID', 'Token address is invalid')
  }

  const key = `launchpad/tokenIcons/${address}.png`

  try {
    const object = await getObjectBytes(key, 1_048_576)
    if (!object) {
      return publicError(404, 'TOKEN_ICON_NOT_FOUND', 'Token icon was not found')
    }
    if (!object.contentType || !IMAGE_TYPES.has(object.contentType)) {
      return publicError(
        502,
        'TOKEN_ICON_TYPE_INVALID',
        'Token icon has an unsupported format',
      )
    }

    return new NextResponse(new Uint8Array(object.body), {
      status: 200,
      headers: {
        'Content-Type': object.contentType,
        'Cache-Control': 'public, max-age=3600, stale-while-revalidate=86400',
        'X-Content-Type-Options': 'nosniff',
      },
    })
  } catch {
    return publicError(
      502,
      'TOKEN_ICON_UNAVAILABLE',
      'Token icon is temporarily unavailable',
    )
  }
}

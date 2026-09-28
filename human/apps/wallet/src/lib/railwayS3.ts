import { S3Client, GetObjectCommand, ListObjectsV2Command } from '@aws-sdk/client-s3'

function requiredEnv(name: string): string {
  const v = process.env[name]
  if (!v) throw new Error(`Missing ${name} in environment`)
  return v
}

function trimSlashes(s: string): string {
  return s.replace(/^\/+/, '').replace(/\/+$/, '')
}

let _client: S3Client | null = null

export function s3Client(): S3Client {
  if (_client) return _client
  const endpoint = requiredEnv('S3_ENDPOINT_URL')
  const region = process.env.S3_REGION || 'auto'

  _client = new S3Client({
    region,
    endpoint,
    forcePathStyle: true,
    credentials: {
      accessKeyId: requiredEnv('S3_ACCESS_KEY_ID'),
      secretAccessKey: requiredEnv('S3_SECRET_ACCESS_KEY'),
    },
  })
  return _client
}

export function s3BucketName(): string {
  return requiredEnv('S3_BUCKET')
}

function errorName(error: unknown): string | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const candidate = error as { name?: unknown; Code?: unknown }
  if (typeof candidate.name === 'string') return candidate.name
  return typeof candidate.Code === 'string' ? candidate.Code : undefined
}

async function streamToBuffer(body: unknown, maxBytes: number): Promise<Buffer> {
  if (!body) return Buffer.alloc(0)
  if (typeof body === 'string') {
    const value = Buffer.from(body)
    if (value.byteLength > maxBytes) throw new Error('S3 object exceeds size limit')
    return value
  }
  if (body instanceof Uint8Array) {
    if (body.byteLength > maxBytes) throw new Error('S3 object exceeds size limit')
    return Buffer.from(body)
  }
  if (
    typeof body !== 'object' ||
    body === null ||
    !(Symbol.asyncIterator in body)
  ) {
    throw new Error('S3 object body is unsupported')
  }
  const chunks: Uint8Array[] = []
  let total = 0
  for await (const chunk of body as AsyncIterable<unknown>) {
    let value: Uint8Array
    if (typeof chunk === 'string') {
      value = new Uint8Array(Buffer.from(chunk))
    } else if (Buffer.isBuffer(chunk)) {
      value = new Uint8Array(chunk)
    } else if (chunk instanceof Uint8Array) {
      value = chunk
    } else {
      throw new Error('S3 object chunk is unsupported')
    }
    total += value.byteLength
    if (total > maxBytes) throw new Error('S3 object exceeds size limit')
    chunks.push(value)
  }
  return Buffer.concat(chunks, total)
}

export async function getObjectBytes(
  key: string,
  maxBytes: number,
): Promise<{ body: Buffer; contentType: string | null } | null> {
  const bucket = s3BucketName()
  const client = s3Client()

  try {
    const res = await client.send(
      new GetObjectCommand({
        Bucket: bucket,
        Key: trimSlashes(key),
      })
    )
    if (
      typeof res.ContentLength === 'number' &&
      res.ContentLength > maxBytes
    ) {
      throw new Error('S3 object exceeds size limit')
    }
    const body = await streamToBuffer(res.Body, maxBytes)
    return body.byteLength === 0
      ? null
      : {
          body,
          contentType: res.ContentType?.split(';')[0]?.trim().toLowerCase() ?? null,
        }
  } catch (error: unknown) {
    const name = errorName(error)
    if (name === 'NoSuchKey' || name === 'NotFound') return null
    throw error
  }
}

export async function getObjectString(
  key: string,
  maxBytes = 65_536,
): Promise<string | null> {
  const object = await getObjectBytes(key, maxBytes)
  return object?.body.toString('utf-8') ?? null
}

export async function listKeys(
  prefix: string,
  maxKeys = 1_000,
): Promise<string[]> {
  if (!Number.isSafeInteger(maxKeys) || maxKeys < 1 || maxKeys > 10_000) {
    throw new TypeError('S3 key limit is invalid')
  }
  const bucket = s3BucketName()
  const client = s3Client()

  const keys: string[] = []
  let continuationToken: string | undefined

  while (true) {
    const res = await client.send(
      new ListObjectsV2Command({
        Bucket: bucket,
        Prefix: trimSlashes(prefix),
        ContinuationToken: continuationToken,
        MaxKeys: Math.min(1_000, maxKeys - keys.length),
      })
    )

    for (const obj of res.Contents || []) {
      if (obj.Key) keys.push(obj.Key)
      if (keys.length >= maxKeys) return keys
    }

    if (!res.IsTruncated) break
    if (!res.NextContinuationToken) {
      throw new Error('S3 listing continuation is invalid')
    }
    continuationToken = res.NextContinuationToken
  }

  return keys
}

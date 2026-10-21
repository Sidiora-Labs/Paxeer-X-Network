'use client'

import { walletParentOrigins } from '@/core/backend-origins'

type JsonRpcRequest = {
  method: string
  params?: unknown[] | Record<string, unknown>
}

type JsonRpcError = {
  code: number
  message: string
  data?: unknown
}

type PostMessageRpcRequest = {
  target: 'sidiora:eip1193'
  type: 'request'
  id: string
  request: JsonRpcRequest
}

type PostMessageRpcResponse = {
  target: 'sidiora:eip1193'
  type: 'response'
  id: string
  result?: unknown
  error?: JsonRpcError
}

type PostMessageRpcEvent = {
  target: 'sidiora:eip1193'
  type: 'event'
  event: string
  params?: unknown
}

type PostMessageRpcReady = {
  target: 'sidiora:eip1193'
  type: 'ready'
}

type ParentMessage = PostMessageRpcResponse | PostMessageRpcEvent | PostMessageRpcReady

type Handler = (...args: any[]) => void

function randomId() {
  return `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null
}

function isParentMessage(v: unknown): v is ParentMessage {
  if (!isObject(v)) return false
  if (v.target !== 'sidiora:eip1193') return false
  if (v.type !== 'response' && v.type !== 'event' && v.type !== 'ready') return false
  return true
}

export type InstallPostMessageEip1193Options = {
  allowedParentOrigins?: string[]
  targetOrigin?: string
  force?: boolean
}

export type Eip1193Provider = {
  request: (args: JsonRpcRequest) => Promise<unknown>
  on: (event: string, handler: Handler) => void
  removeListener: (event: string, handler: Handler) => void
  isMetaMask?: boolean
  isCoinbaseWallet?: boolean
  isBraveWallet?: boolean
  isRainbow?: boolean
  isSidioraPostMessage?: boolean
}

class PostMessageEip1193Provider implements Eip1193Provider {
  public isSidioraPostMessage = true

  private handlers = new Map<string, Set<Handler>>()
  private inflight = new Map<
    string,
    {
      resolve: (v: unknown) => void
      reject: (e: unknown) => void
      timeout: ReturnType<typeof setTimeout>
    }
  >()

  constructor(
    private readonly opts: Required<Pick<InstallPostMessageEip1193Options, 'allowedParentOrigins' | 'targetOrigin'>>
  ) {
    this.onMessage = this.onMessage.bind(this)
    window.addEventListener('message', this.onMessage)

    try {
      window.parent?.postMessage(
        { target: 'sidiora:eip1193', type: 'ready' } satisfies PostMessageRpcReady,
        this.opts.targetOrigin,
      )
    } catch {
      // ignore
    }
  }

  destroy() {
    window.removeEventListener('message', this.onMessage)
    for (const [, item] of this.inflight) clearTimeout(item.timeout)
    this.inflight.clear()
    this.handlers.clear()
  }

  on(event: string, handler: Handler) {
    const set = this.handlers.get(event) || new Set<Handler>()
    set.add(handler)
    this.handlers.set(event, set)
  }

  removeListener(event: string, handler: Handler) {
    const set = this.handlers.get(event)
    if (!set) return
    set.delete(handler)
    if (set.size === 0) this.handlers.delete(event)
  }

  private emitLocal(event: string, ...args: any[]) {
    const set = this.handlers.get(event)
    if (!set) return
    for (const fn of set) {
      try {
        fn(...args)
      } catch {
        // ignore listener errors
      }
    }
  }

  async request(args: JsonRpcRequest): Promise<unknown> {
    if (!window.parent || window.parent === window) {
      throw new Error('postMessage provider requires being inside an iframe')
    }

    const id = randomId()
    const msg: PostMessageRpcRequest = {
      target: 'sidiora:eip1193',
      type: 'request',
      id,
      request: args,
    }

    return await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.inflight.delete(id)
        reject(new Error(`postMessage request timeout: ${args.method}`))
      }, 60_000)

      this.inflight.set(id, { resolve, reject, timeout })

      try {
        window.parent.postMessage(msg, this.opts.targetOrigin)
      } catch (e) {
        clearTimeout(timeout)
        this.inflight.delete(id)
        reject(e)
      }
    })
  }

  private originAllowed(origin: string): boolean {
    if (!this.opts.allowedParentOrigins.length) return true
    return this.opts.allowedParentOrigins.includes(origin)
  }

  private onMessage(event: MessageEvent) {
    if (!this.originAllowed(event.origin)) return
    if (!isParentMessage(event.data)) return

    if (event.data.type === 'response') {
      const pending = this.inflight.get(event.data.id)
      if (!pending) return

      clearTimeout(pending.timeout)
      this.inflight.delete(event.data.id)

      if (event.data.error) {
        const err = new Error(event.data.error.message)
        ;(err as any).code = event.data.error.code
        ;(err as any).data = event.data.error.data
        pending.reject(err)
        return
      }

      pending.resolve(event.data.result)
      return
    }

    if (event.data.type === 'event') {
      this.emitLocal(event.data.event, event.data.params)
      return
    }

    if (event.data.type === 'ready') {
      this.emitLocal('ready')
    }
  }
}

function getDefaultTargetOrigin(allowedParentOrigins?: string[], targetOrigin?: string): string {
  if (targetOrigin) return targetOrigin
  if (allowedParentOrigins && allowedParentOrigins.length === 1) return allowedParentOrigins[0]
  return '*'
}

export function installPostMessageEip1193Provider(options?: InstallPostMessageEip1193Options) {
  if (typeof window === 'undefined') return null

  if (window.parent === window) return null

  const existing = (window as any).ethereum as Eip1193Provider | undefined
  if (existing && existing.isSidioraPostMessage) {
    return existing as PostMessageEip1193Provider
  }
  if (existing && !options?.force) return null

  // Trusted parents are NEXT_PUBLIC_WALLET_IFRAME_ORIGIN through the shared
  // resolver, the same list the CSP frame-ancestors allows (src/middleware.ts).
  // With none configured the app cannot be framed by a wallet, and an empty
  // list here would trust any parent, so the bridge is not installed.
  const allowedParentOrigins = options?.allowedParentOrigins ?? walletParentOrigins()
  if (allowedParentOrigins.length === 0) return null

  const targetOrigin = getDefaultTargetOrigin(allowedParentOrigins, options?.targetOrigin)

  const provider = new PostMessageEip1193Provider({
    allowedParentOrigins,
    targetOrigin,
  })

  ;(window as any).ethereum = provider

  try {
    window.dispatchEvent(new Event('ethereum#initialized'))
  } catch {
    // ignore
  }

  return provider
}

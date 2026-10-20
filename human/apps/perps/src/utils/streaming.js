import { fetchWithPolicy as fetch } from "@/core/net/fetch";
// Assuming you're working in a browser environment that supports fetch and ReadableStream
const streamingUrl = 'https://benchmarks.pyth.network/v1/shims/tradingview/streaming'
const channelToSubscription = new Map()
let isConnected = false
let heartbeatInterval = null
let reconnectTimeout = null
const INITIAL_RECONNECT_DELAY = 1000
const MAX_RECONNECT_DELAY = 30000
let currentReconnectDelay = INITIAL_RECONNECT_DELAY

function handleStreamingData(data) {
  const { id, p, t } = data

  const tradePrice = p
  const tradeTime = t * 1000 // Multiplying by 1000 to get milliseconds

  const channelString = id
  const subscriptionItem = channelToSubscription.get(channelString)

  if (!subscriptionItem) {
    return
  }

  const lastDailyBar = subscriptionItem.lastDailyBar
  const nextDailyBarTime = getNextDailyBarTime(lastDailyBar.time)

  let bar
  if (tradeTime >= nextDailyBarTime) {
    bar = {
      time: nextDailyBarTime,
      open: tradePrice,
      high: tradePrice,
      low: tradePrice,
      close: tradePrice,
    }
 } else {
    bar = {
      ...lastDailyBar,
      high: Math.max(lastDailyBar.high, tradePrice),
      low: Math.min(lastDailyBar.low, tradePrice),
      close: tradePrice,
    }
  }

  subscriptionItem.lastDailyBar = bar

  // Send data to every subscriber of that symbol
  subscriptionItem.handlers.forEach((handler) => handler.callback(bar))
  channelToSubscription.set(channelString, subscriptionItem)
}

function startHeartbeat() {
  clearInterval(heartbeatInterval)
  heartbeatInterval = setInterval(() => {
    if (!isConnected) {
      console.warn('[stream] Connection lost, attempting to reconnect...')
      reconnectWithBackoff()
    }
  }, 5000) // Check connection every 5 seconds
}

function reconnectWithBackoff() {
  clearTimeout(reconnectTimeout)
  if (currentReconnectDelay < MAX_RECONNECT_DELAY) {
    currentReconnectDelay *= 2 // Exponential backoff
  }

  reconnectTimeout = setTimeout(() => {
    console.log(`[stream] Attempting to reconnect...`)
    startStreaming()
  }, currentReconnectDelay)
}

function startStreaming(retries = 3, delay = 3000) {
  // Reset reconnect delay on successful connection
  currentReconnectDelay = INITIAL_RECONNECT_DELAY
  clearInterval(heartbeatInterval)
  clearTimeout(reconnectTimeout)

  fetch(streamingUrl, { stream: true })
    .then((response) => {
      isConnected = true
      startHeartbeat()
      const reader = response.body.getReader()

      function streamData() {
        reader
          .read()
          .then(({ value, done }) => {
            if (done) {
              console.error('[stream] Streaming ended.')
              isConnected = false
              reconnectWithBackoff()
              return
            }

            // Assuming the streaming data is separated by line breaks
            const dataStrings = new TextDecoder().decode(value).split('\n')
            dataStrings.forEach((dataString) => {
              const trimmedDataString = dataString.trim()
              if (trimmedDataString) {
                try {
                  var jsonData = JSON.parse(trimmedDataString)
                  handleStreamingData(jsonData)
                } catch (e) {
                  // Error parsing JSON - silently continue
                }
              }
            })

            streamData() // Continue processing the stream
          })
          .catch((error) => {
            console.error('[stream] Error reading from stream:', error)
            isConnected = false
            reconnectWithBackoff()
          })
      }

      streamData()
    })
    .catch((error) => {
      console.error(
        '[stream] Error fetching from the streaming endpoint:',
        error
      )
      isConnected = false
      reconnectWithBackoff()
    })
  function attemptReconnect(retriesLeft, delay) {
    if (retriesLeft > 0) {
      console.log(`[stream] Attempting to reconnect in ${delay}ms...`)
      setTimeout(() => {
        startStreaming(retriesLeft - 1, delay)
      }, delay)
    } else {
      console.error('[stream] Maximum reconnection attempts reached.')
    }
  }
}

function getNextDailyBarTime(barTime) {
  const date = new Date(barTime * 1000)
  date.setDate(date.getDate() + 1)
  return date.getTime() / 1000
}

export function subscribeOnStream(
  symbolInfo,
  resolution,
  onRealtimeCallback,
  subscriberUID,
  onResetCacheNeededCallback,
  lastDailyBar
) {
  const channelString = symbolInfo.ticker
  const handler = {
    id: subscriberUID,
    callback: onRealtimeCallback,
  }
  let subscriptionItem = channelToSubscription.get(channelString)
  subscriptionItem = {
    subscriberUID,
    resolution,
    lastDailyBar,
    handlers: [handler],
  }
  channelToSubscription.set(channelString, subscriptionItem)

  // Only start streaming if not already connected
  if (!isConnected) {
    startStreaming()
  }
}

export function unsubscribeFromStream(subscriberUID) {
  // Find a subscription with id === subscriberUID
  for (const channelString of channelToSubscription.keys()) {
    const subscriptionItem = channelToSubscription.get(channelString)
    const handlerIndex = subscriptionItem.handlers.findIndex(
      (handler) => handler.id === subscriberUID
    )

    if (handlerIndex !== -1) {
      // Unsubscribe from the channel if it is the last handler
      console.log(
        '[unsubscribeBars]: Unsubscribe from streaming. Channel:',
        channelString
      )
      channelToSubscription.delete(channelString)
      break
    }
  }

  // If no more subscriptions, clear intervals and connection state
  if (channelToSubscription.size === 0) {
    clearInterval(heartbeatInterval)
    clearTimeout(reconnectTimeout)
    isConnected = false
  }
}

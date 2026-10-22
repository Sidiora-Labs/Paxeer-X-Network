defmodule Indexer.Fetcher.PaxeerX.PriceUpdates do
  @moduledoc """
  Scans blocks for native price-feed submissions and stores them through
  `Explorer.Chain.PaxeerX.PriceFeed`.

  Realtime: every block the block fetcher imports at or above the start block is
  buffered, so a refetched block is scanned again and replaces what was stored for its
  height. Catch-up: on start the consensus heights at or above the start block whose
  scan failed are buffered, then those that have no scanned row, each lowest first.

  A height the nodes no longer serve is recorded as unavailable and not retried. Any
  other failure is buffered again as `{number, attempts, not_before, failures}` once a
  doubling delay has passed. A node whose head is still below the height or a node that
  cannot be reached is transient and retried for as long as it lasts; after
  `@max_attempts` other failures the height is recorded as failed with its last error and
  scanned again by the next catch-up. Either record is logged once.

  Once a block's submissions are committed, from either path, the height is published as a
  realtime `:paxeer_x_price_updates` chain event so the web notifier streams them.

  The market list is read from the price-feed precompile with `getMarketCount()` and
  `getMarket(uint256)` over the indexer's JSON-RPC when it is older than the refresh
  interval or a submission names a market it does not hold, and only for a block that
  carries a submission. Attempts are spaced by the refresh interval across every batch,
  so a market the precompile does not list costs no calls per block.
  """

  use Indexer.Fetcher, restart: :permanent

  require Logger

  import Ecto.Query, only: [from: 2]

  alias EthereumJSONRPC.Contract
  alias Explorer.Chain.Block
  alias Explorer.Chain.Events.Publisher
  alias Explorer.Chain.PaxeerX.PriceFeed
  alias Explorer.PaxeerX.CosmosRest
  alias Explorer.Repo
  alias Indexer.BufferedTask
  alias Indexer.Fetcher.PaxeerX.PriceUpdates.Supervisor, as: PriceUpdatesSupervisor

  @behaviour BufferedTask

  @default_precompile "0x0000000000000000000000000000000000001020"
  @get_market_count "fd69f3c2"
  @get_market "eb44fdd3"
  @max_markets 1024
  @max_attempts 10
  @max_backoff_ms 300_000
  @never -9_223_372_036_854_775_808
  @transient [:height_ahead, :unreachable]

  @abi [
    %{
      "type" => "function",
      "name" => "getMarketCount",
      "stateMutability" => "view",
      "inputs" => [],
      "outputs" => [%{"name" => "", "type" => "uint256"}]
    },
    %{
      "type" => "function",
      "name" => "getMarket",
      "stateMutability" => "view",
      "inputs" => [%{"name" => "marketId", "type" => "uint256"}],
      "outputs" => [
        %{
          "name" => "",
          "type" => "tuple",
          "components" => [
            %{"name" => "marketId", "type" => "uint64"},
            %{"name" => "symbol", "type" => "string"},
            %{"name" => "chainflowSymbol", "type" => "string"},
            %{"name" => "minPrice", "type" => "uint256"},
            %{"name" => "maxPrice", "type" => "uint256"},
            %{"name" => "enabled", "type" => "bool"}
          ]
        }
      ]
    }
  ]

  @doc """
  Buffers the consensus blocks at or above the start block for scanning.
  """
  @spec async_fetch([Block.t()], boolean()) :: :ok
  def async_fetch(blocks, realtime?) when is_list(blocks) do
    if PriceUpdatesSupervisor.disabled?() do
      :ok
    else
      start_block = Application.get_env(:indexer, __MODULE__)[:start_block]

      blocks
      |> Enum.filter(&(&1.consensus and &1.number >= start_block))
      |> Enum.map(& &1.number)
      |> case do
        [] -> :ok
        numbers -> BufferedTask.buffer(__MODULE__, numbers, realtime?)
      end
    end
  end

  @doc false
  def child_spec([init_options, gen_server_options]) do
    {json_rpc_named_arguments, init_options} = Keyword.pop(init_options, :json_rpc_named_arguments)

    merged_init_options =
      defaults()
      |> Keyword.merge(init_options)
      |> Keyword.put(:state, state(json_rpc_named_arguments))

    Supervisor.child_spec({BufferedTask, [{__MODULE__, merged_init_options}, gen_server_options]}, id: __MODULE__)
  end

  @doc """
  The fetcher state from the application environment.
  """
  @spec state(EthereumJSONRPC.json_rpc_named_arguments()) :: map()
  def state(json_rpc_named_arguments) do
    config = Application.get_env(:indexer, __MODULE__)

    %{
      json_rpc_named_arguments: json_rpc_named_arguments,
      cosmos_rest_urls: config[:cosmos_rest_urls],
      cometbft_rpc_urls: config[:cometbft_rpc_urls],
      start_block: config[:start_block],
      precompile: config[:precompile] || @default_precompile,
      markets_refresh_seconds: config[:markets_refresh_seconds],
      markets_attempted_at: refresh_clock()
    }
  end

  @doc """
  The market-refresh attempt time shared by every batch, unset until the first attempt.
  """
  @spec refresh_clock() :: :atomics.atomics_ref()
  def refresh_clock do
    clock = :atomics.new(1, signed: true)
    :atomics.put(clock, 1, @never)
    clock
  end

  @impl BufferedTask
  def init(initial, reducer, state) do
    {:ok, final} = PriceFeed.stream_unscanned_block_numbers(initial, reducer, state.start_block)
    final
  end

  @impl BufferedTask
  def run(entries, state) do
    now = System.monotonic_time(:millisecond)

    Enum.each(entries, fn entry ->
      case normalize(entry) do
        {_number, _attempts, not_before, _failures} = entry when is_integer(not_before) and not_before > now ->
          schedule(entry, not_before - now)

        {number, attempts, _not_before, failures} ->
          number |> scan_block(state) |> after_scan(number, attempts + 1, failures, now)
      end
    end)
  end

  defp normalize(number) when is_integer(number), do: {number, 0, nil, 0}
  defp normalize({_number, _attempts, _not_before, _failures} = entry), do: entry

  defp after_scan(:ok, _number, _attempts, _failures, _now), do: :ok

  defp after_scan({:error, {:height_unavailable, _} = reason}, number, _attempts, _failures, _now) do
    Logger.warning("price feed block #{number} is no longer served by the configured nodes: #{inspect(reason)}")
    PriceFeed.mark_unscannable(number, "unavailable", inspect(reason))
  end

  defp after_scan({:error, {kind, _} = reason}, number, attempts, failures, now) when kind in @transient do
    retry(number, attempts, failures, now, reason)
  end

  defp after_scan({:error, reason}, number, _attempts, failures, _now) when failures + 1 >= @max_attempts do
    Logger.error("price feed scan of block #{number} failed #{failures + 1} times: #{inspect(reason)}")
    PriceFeed.mark_unscannable(number, "failed", inspect(reason))
  end

  defp after_scan({:error, reason}, number, attempts, failures, now) do
    retry(number, attempts, failures + 1, now, reason)
  end

  defp retry(number, attempts, failures, now, reason) do
    Logger.debug("price feed scan of block #{number} failed, attempt #{attempts}: #{inspect(reason)}")
    delay = backoff_ms(attempts)
    schedule({number, attempts, now + delay, failures}, delay)
  end

  # The entry stays off the queue until it is due, so a waiting retry runs no batch.
  # A retry still waiting when the fetcher restarts is buffered again by catch-up.
  defp schedule(entry, delay) do
    Process.send_after(__MODULE__, {:buffer, [entry], false}, delay)
    :ok
  end

  @doc false
  def backoff_ms(attempts), do: min(1_000 * Integer.pow(2, min(attempts, 20) - 1), @max_backoff_ms)

  defp scan_block(number, state) do
    case Repo.one(from(block in Block, where: block.number == ^number and block.consensus == true)) do
      nil ->
        :ok

      block ->
        with {:ok, updates} <- CosmosRest.block_price_updates(state.cosmos_rest_urls, state.cometbft_rpc_urls, number),
             :ok <- maybe_refresh_markets(updates, state),
             {:ok, count} <- PriceFeed.replace_block_updates(block, updates) do
          if count > 0, do: Publisher.broadcast(%{paxeer_x_price_updates: [number]}, :realtime)
          :ok
        end
    end
  end

  defp maybe_refresh_markets([], _state), do: :ok

  defp maybe_refresh_markets(updates, state) do
    now = System.monotonic_time(:millisecond)
    attempted_at = :atomics.get(state.markets_attempted_at, 1)

    if now - attempted_at >= state.markets_refresh_seconds * 1_000 do
      market_ids = for update <- updates, observation <- update.observations, do: observation.market_id
      refreshed_at = PriceFeed.markets_refreshed_at()

      stale? =
        is_nil(refreshed_at) or
          DateTime.diff(DateTime.utc_now(), refreshed_at) >= state.markets_refresh_seconds

      # Only the batch that moves the shared attempt time refreshes.
      if (stale? or PriceFeed.unknown_market_ids(market_ids) != []) and
           :atomics.compare_exchange(state.markets_attempted_at, 1, attempted_at, now) == :ok do
        refresh_and_report(market_ids, state)
      end
    end

    :ok
  end

  defp refresh_and_report(market_ids, state) do
    case refresh_markets(state) do
      {:ok, _count} ->
        case PriceFeed.unknown_market_ids(market_ids) do
          [] -> :ok
          unknown -> Logger.warning("price feed markets #{inspect(unknown)} are still unknown after a refresh")
        end

      {:error, reason} ->
        Logger.error("price feed market refresh failed: #{inspect(reason)}")
    end
  end

  @doc """
  Reads every market from the price-feed precompile and upserts it. A market id the
  precompile does not list is skipped.
  """
  @spec refresh_markets(map()) :: {:ok, non_neg_integer()} | {:error, term()}
  def refresh_markets(state) do
    case call(state, [%{contract_address: state.precompile, method_id: @get_market_count, args: []}]) do
      [{:ok, [count]}] when is_integer(count) and count >= 0 and count <= @max_markets ->
        markets =
          if count == 0 do
            []
          else
            0..(count - 1)
            |> Enum.map(&%{contract_address: state.precompile, method_id: @get_market, args: [&1]})
            |> then(&call(state, &1))
            |> Enum.flat_map(fn
              {:ok, [{market_id, symbol, chainflow_symbol, _min, _max, _enabled}]} ->
                [%{market_id: market_id, symbol: symbol, chainflow_symbol: chainflow_symbol}]

              _ ->
                []
            end)
          end

        {:ok, PriceFeed.upsert_markets(markets)}

      [{:ok, other}] ->
        {:error, {:market_count_out_of_range, other}}

      [{:error, reason}] ->
        {:error, reason}
    end
  end

  defp call(state, requests), do: Contract.execute_contract_functions(requests, @abi, state.json_rpc_named_arguments)

  defp defaults do
    config = Application.get_env(:indexer, __MODULE__, [])

    [
      poll: false,
      flush_interval: :timer.seconds(1),
      max_batch_size: 10,
      max_concurrency: config[:concurrency] || 4,
      task_supervisor: Indexer.Fetcher.PaxeerX.PriceUpdates.TaskSupervisor,
      metadata: [fetcher: :paxeer_x_price_updates]
    ]
  end
end

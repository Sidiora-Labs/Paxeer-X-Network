defmodule Indexer.Fetcher.PaxeerX.PriceFeedMaintenance do
  @moduledoc """
  Keeps the price-feed tables bounded while the price feed is indexed.

  Every interval it advances the scanned watermark and deletes the scanned rows behind
  it, a bounded batch at a time, so catch-up reads only the heights above the
  watermark. When a retention period is configured it also deletes the submissions older
  than that period, with their observations, oldest first in bounded batches. Without a
  retention period every submission is kept.
  """

  use GenServer

  require Logger

  alias Explorer.Chain.PaxeerX.PriceFeed
  alias Indexer.Fetcher.PaxeerX.PriceUpdates

  @default_interval :timer.minutes(1)
  @scanned_batch_size 10_000
  @retention_batch_size 1_000
  @max_batches 20

  def start_link(args) do
    GenServer.start_link(__MODULE__, args, name: __MODULE__)
  end

  @impl GenServer
  def init(_args) do
    send(self(), :maintain)
    {:ok, nil}
  end

  @impl GenServer
  def handle_info(:maintain, state) do
    config = Application.get_env(:indexer, __MODULE__, [])

    try do
      run_once(Application.get_env(:indexer, PriceUpdates)[:start_block], config[:retention_days])
    rescue
      error -> Logger.error("price feed maintenance failed: #{Exception.message(error)}")
    end

    Process.send_after(self(), :maintain, config[:interval] || @default_interval)
    {:noreply, state}
  end

  @doc """
  One maintenance pass: compacts the scanned heights from `start_block` on and, when
  `retention_days` is set, deletes the submissions older than it. Each part stops after
  `@max_batches` batches and continues on the next pass.
  """
  @spec run_once(non_neg_integer(), pos_integer() | nil) :: %{scanned_through: integer(), expired: non_neg_integer()}
  def run_once(start_block, retention_days) do
    %{scanned_through: compact(start_block, @max_batches), expired: expire(retention_days, @max_batches, 0)}
  end

  defp compact(start_block, batches) do
    through = PriceFeed.scanned_through(start_block)
    new_through = PriceFeed.compact_scanned_blocks(start_block, @scanned_batch_size)

    if new_through - through == @scanned_batch_size and batches > 1,
      do: compact(start_block, batches - 1),
      else: new_through
  end

  defp expire(nil, _batches, deleted), do: deleted

  defp expire(retention_days, batches, deleted) do
    count = PriceFeed.delete_expired_updates(retention_days, @retention_batch_size)

    if count == @retention_batch_size and batches > 1,
      do: expire(retention_days, batches - 1, deleted + count),
      else: deleted + count
  end
end

defmodule Explorer.Chain.PaxeerX.PriceFeed do
  @moduledoc """
  Reads and writes native price-feed submissions.

  Reads return only submissions of consensus blocks, newest first by
  `(block_number, index)`, with their observations preloaded in market order. Paging is
  keyset paging on `Explorer.PagingOptions`: `key` is the `{block_number, index}` of the
  last item already shown, or `nil` for the first page.

  Writes replace a whole block at a time: `replace_block_updates/2` deletes the updates
  stored for the block's height, inserts the ones just read and marks the height scanned
  in one transaction, so rescanning a refetched block leaves no stale row behind.

  Scanned heights are compacted behind a watermark: `compact_scanned_blocks/2` advances
  the height through which every height has a scanned row and deletes the plain scanned
  rows at or below it, so catch-up reads only the heights above the watermark and the
  table holds the recent heights and the unscannable ones. `delete_expired_updates/2`
  deletes the oldest submissions, with their observations, once they are older than the
  retention period.
  """

  import Ecto.Query

  alias Explorer.Chain.{Block, Hash}
  alias Explorer.Chain.Cache.Counters.LastFetchedCounter
  alias Explorer.Chain.PaxeerX.{PriceFeedScannedBlock, PriceMarket, PriceObservation, PriceUpdate}
  alias Explorer.{PagingOptions, Repo}

  @default_page_size 50

  @type filters :: [feeder: Hash.Address.t() | nil, market_id: non_neg_integer() | nil]

  @doc """
  A page of submissions, optionally only those of one feeder or carrying one market.
  """
  @spec list_updates(PagingOptions.t(), filters()) :: [PriceUpdate.t()]
  def list_updates(%PagingOptions{} = paging_options, filters \\ []) do
    paging_options
    |> list_updates_query(filters)
    |> preload_observations()
    |> Repo.all()
  end

  @doc false
  def list_updates_query(%PagingOptions{} = paging_options, filters \\ []) do
    consensus_updates_query()
    |> filter_feeder(filters[:feeder])
    |> filter_market(filters[:market_id])
    |> page_updates(paging_options)
    |> limit(^page_size(paging_options))
  end

  @doc """
  The submission with the given hash, or `nil`.
  """
  @spec update_by_hash(Hash.Full.t() | String.t()) :: PriceUpdate.t() | nil
  def update_by_hash(hash) do
    case Hash.Full.cast(hash) do
      {:ok, hash} ->
        hash
        |> update_by_hash_query()
        |> preload_observations()
        |> Repo.one()

      :error ->
        nil
    end
  end

  @doc false
  def update_by_hash_query(%Hash{} = hash), do: where(consensus_updates_query(), [update], update.hash == ^hash)

  @doc """
  Every known market in id order with its latest consensus observation and that
  observation's submission preloaded, or `nil` when the market has none.

  Each market reads only its newest observations through the `(market_id, block_number
  desc)` index, so the cost follows the number of markets, not the stored history.
  """
  @spec latest_per_market() :: [{PriceMarket.t(), PriceObservation.t() | nil}]
  def latest_per_market do
    rows = Repo.all(latest_per_market_query())

    latest =
      rows
      |> Enum.flat_map(fn
        {_market, %PriceObservation{update_hash: %Hash{}} = observation} -> [observation]
        _ -> []
      end)
      |> Repo.preload(:update)
      |> Map.new(&{&1.market_id, &1})

    Enum.map(rows, fn {market, _} -> {market, Map.get(latest, market.market_id)} end)
  end

  @doc false
  def latest_per_market_query do
    newest =
      from(observation in PriceObservation,
        inner_join: update in assoc(observation, :update),
        inner_join: block in Block,
        on: block.hash == update.block_hash and block.consensus == true,
        where: observation.market_id == parent_as(:market).market_id,
        order_by: [desc: observation.block_number, desc: update.index],
        limit: 1,
        select: observation
      )

    from(market in PriceMarket,
      as: :market,
      left_lateral_join: newest in subquery(newest),
      on: true,
      order_by: [asc: market.market_id],
      select: {market, newest}
    )
  end

  @doc """
  A page of one market's observations, newest first, each with its submission preloaded.
  Paging walks the `(market_id, block_number desc)` index: `key` is the
  `{block_number, index}` of the last item shown.
  """
  @spec market_updates(non_neg_integer(), PagingOptions.t()) :: [PriceObservation.t()]
  def market_updates(market_id, %PagingOptions{} = paging_options) when is_integer(market_id) do
    market_updates_query(market_id, paging_options)
    |> Repo.all()
  end

  @doc false
  def market_updates_query(market_id, %PagingOptions{} = paging_options) do
    from(observation in PriceObservation,
      inner_join: update in assoc(observation, :update),
      inner_join: block in Block,
      on: block.hash == update.block_hash and block.consensus == true,
      where: observation.market_id == ^market_id,
      order_by: [desc: observation.block_number, desc: update.index],
      preload: [update: update]
    )
    |> page_observations(paging_options)
    |> limit(^page_size(paging_options))
  end

  @doc """
  The submissions of the given heights, newest first.
  """
  @spec updates_in_blocks([non_neg_integer()]) :: [PriceUpdate.t()]
  def updates_in_blocks(block_numbers) when is_list(block_numbers) do
    block_numbers
    |> updates_in_blocks_query()
    |> preload_observations()
    |> Repo.all()
  end

  @doc false
  def updates_in_blocks_query(block_numbers),
    do: where(consensus_updates_query(), [update], update.block_number in ^block_numbers)

  @doc """
  The submissions of one height in transaction order, newest first.
  """
  @spec block_updates(non_neg_integer()) :: [PriceUpdate.t()]
  def block_updates(block_number) when is_integer(block_number), do: updates_in_blocks([block_number])

  @doc """
  Stores the submissions read for one consensus block and marks its height scanned.

  `updates` are the maps `Explorer.PaxeerX.CosmosRest.block_price_updates/3` returns.
  """
  @spec replace_block_updates(Block.t(), [map()]) :: {:ok, non_neg_integer()} | {:error, term()}
  def replace_block_updates(%Block{number: number, hash: block_hash, timestamp: timestamp}, updates) do
    now = DateTime.utc_now()

    updates = Enum.map(updates, &cast_hashes/1)

    update_rows =
      Enum.map(updates, fn update ->
        update
        |> Map.take(~w(hash index feeder_hash feeder_bech32 status gas_used gas_wanted)a)
        |> Map.merge(%{
          block_number: number,
          block_hash: block_hash,
          observation_count: length(update.observations),
          timestamp: timestamp,
          inserted_at: now,
          updated_at: now
        })
      end)

    observation_rows =
      Enum.flat_map(updates, fn update ->
        Enum.map(update.observations, &Map.merge(&1, %{update_hash: update.hash, block_number: number}))
      end)

    Repo.transaction(fn ->
      Repo.delete_all(from(update in PriceUpdate, where: update.block_number == ^number))
      {count, _} = Repo.insert_all(PriceUpdate, update_rows)
      Repo.insert_all(PriceObservation, observation_rows)

      mark_scanned(number, "scanned", nil)

      count
    end)
  end

  @doc """
  Records that the height could not be scanned: `"unavailable"` when the nodes no longer
  serve it, `"failed"` when its scan kept failing. Catch-up then skips the height until a
  rescan or a refetch stores it.
  """
  @spec mark_unscannable(non_neg_integer(), String.t(), String.t()) :: :ok
  def mark_unscannable(number, status, error) when status in ["unavailable", "failed"] do
    mark_scanned(number, status, error)
    :ok
  end

  defp mark_scanned(number, status, error) do
    Repo.insert_all(PriceFeedScannedBlock, [%{block_number: number, status: status, error: error}],
      on_conflict: {:replace, [:status, :error]},
      conflict_target: :block_number
    )
  end

  @doc """
  Inserts or refreshes market rows from `%{market_id, symbol, chainflow_symbol}` maps.
  """
  @spec upsert_markets([map()]) :: non_neg_integer()
  def upsert_markets(markets) do
    now = DateTime.utc_now()
    rows = Enum.map(markets, &Map.put(Map.take(&1, ~w(market_id symbol chainflow_symbol)a), :refreshed_at, now))

    {count, _} =
      Repo.insert_all(PriceMarket, rows,
        on_conflict: {:replace, [:symbol, :chainflow_symbol, :refreshed_at]},
        conflict_target: :market_id
      )

    count
  end

  @doc """
  When the market list was last refreshed, or `nil` before the first refresh.
  """
  @spec markets_refreshed_at() :: DateTime.t() | nil
  def markets_refreshed_at, do: Repo.one(from(market in PriceMarket, select: max(market.refreshed_at)))

  @doc """
  The market ids among `market_ids` that have no market row.
  """
  @spec unknown_market_ids([non_neg_integer()]) :: [non_neg_integer()]
  def unknown_market_ids(market_ids) do
    known = Repo.all(from(market in PriceMarket, where: market.market_id in ^market_ids, select: market.market_id))
    Enum.uniq(market_ids) -- known
  end

  @doc """
  Reduces over the consensus block heights at or above `start_block` that have not been
  scanned yet, lowest first. Only the heights above the scanned watermark are read.
  """
  @spec stream_unscanned_block_numbers(acc, (non_neg_integer(), acc -> acc), non_neg_integer()) :: {:ok, acc}
        when acc: term()
  def stream_unscanned_block_numbers(initial, reducer, start_block) when is_function(reducer, 2) do
    start_block
    |> scanned_through()
    |> Kernel.+(1)
    |> unscanned_block_numbers_query()
    |> Repo.stream_reduce(initial, reducer)
  end

  @doc false
  def unscanned_block_numbers_query(from_block) do
    from(block in Block,
      left_join: scanned in PriceFeedScannedBlock,
      on: scanned.block_number == block.number,
      where: block.consensus == true and block.number >= ^from_block and is_nil(scanned.block_number),
      order_by: [asc: block.number],
      select: block.number
    )
  end

  @doc """
  The height through which every height from `start_block` on has a scanned row, or
  `start_block - 1` before the first compaction. The watermark is kept per start block,
  so a changed start block is scanned from its own start.
  """
  @spec scanned_through(non_neg_integer()) :: integer()
  def scanned_through(start_block) do
    case LastFetchedCounter.get(scanned_through_counter(start_block), nullable: true) do
      nil -> start_block - 1
      value -> max(Decimal.to_integer(value), start_block - 1)
    end
  end

  @doc """
  Advances the scanned watermark over the next at most `batch_size` contiguous heights
  that have a scanned row, and deletes the rows of those heights whose status is
  `"scanned"`; unavailable and failed rows stay for a rescan. Returns the new watermark.
  """
  @spec compact_scanned_blocks(non_neg_integer(), pos_integer()) :: integer()
  def compact_scanned_blocks(start_block, batch_size) when is_integer(batch_size) and batch_size > 0 do
    through = scanned_through(start_block)

    case Repo.one(contiguous_scanned_query(through, batch_size)) do
      nil ->
        through

      new_through ->
        {:ok, _} =
          Repo.transaction(fn ->
            {:ok, _} =
              LastFetchedCounter.upsert(%{counter_type: scanned_through_counter(start_block), value: new_through})

            Repo.delete_all(
              from(scanned in PriceFeedScannedBlock,
                where:
                  scanned.block_number > ^through and scanned.block_number <= ^new_through and
                    scanned.status == "scanned"
              )
            )
          end)

        new_through
    end
  end

  @doc false
  def contiguous_scanned_query(through, batch_size) do
    next =
      from(scanned in PriceFeedScannedBlock,
        where: scanned.block_number > ^through,
        order_by: [asc: scanned.block_number],
        limit: ^batch_size,
        select: %{block_number: scanned.block_number}
      )

    numbered =
      from(scanned in subquery(next),
        select: %{
          block_number: scanned.block_number,
          gap: scanned.block_number - over(row_number(), order_by: scanned.block_number)
        }
      )

    from(scanned in subquery(numbered), where: scanned.gap == ^through, select: max(scanned.block_number))
  end

  defp scanned_through_counter(start_block), do: "paxeer_x_price_feed_scanned_through_#{start_block}"

  @doc """
  Deletes, oldest first, at most `batch_size` submissions whose block is older than
  `retention_days` days, with their observations. Returns how many were deleted; fewer
  than `batch_size` means nothing older is left.
  """
  @spec delete_expired_updates(pos_integer(), pos_integer()) :: non_neg_integer()
  def delete_expired_updates(retention_days, batch_size)
      when is_integer(retention_days) and retention_days > 0 and is_integer(batch_size) and batch_size > 0 do
    cutoff = DateTime.add(DateTime.utc_now(), -retention_days * 86_400, :second)

    case for(
           {hash, timestamp} <- Repo.all(oldest_updates_query(batch_size)),
           DateTime.compare(timestamp, cutoff) == :lt,
           do: hash
         ) do
      [] ->
        0

      hashes ->
        {count, _} = Repo.delete_all(from(update in PriceUpdate, where: update.hash in ^hashes))
        count
    end
  end

  @doc false
  def oldest_updates_query(batch_size) do
    from(update in PriceUpdate,
      order_by: [asc: update.block_number, asc: update.index],
      limit: ^batch_size,
      select: {update.hash, update.timestamp}
    )
  end

  @doc """
  Whether any block height has been scanned for submissions, by this node or by any
  indexer writing to the same database.
  """
  @spec scanned_any?() :: boolean()
  def scanned_any?, do: Repo.exists?(PriceFeedScannedBlock)

  @doc """
  The symbol of each known market among `market_ids`, by market id.
  """
  @spec market_symbols([non_neg_integer()]) :: %{non_neg_integer() => String.t()}
  def market_symbols([]), do: %{}

  def market_symbols(market_ids) do
    PriceMarket
    |> where([market], market.market_id in ^Enum.uniq(market_ids))
    |> select([market], {market.market_id, market.symbol})
    |> Repo.all()
    |> Map.new()
  end

  @doc """
  The symbol of each known market the given submissions carry an observation for, by
  market id.
  """
  @spec update_market_symbols([PriceUpdate.t()]) :: %{non_neg_integer() => String.t()}
  def update_market_symbols(updates) do
    updates
    |> Enum.flat_map(fn update -> Enum.map(update.observations, & &1.market_id) end)
    |> market_symbols()
  end

  @doc """
  How many submissions the block with the given hash carries.
  """
  @spec block_update_count(Hash.Full.t()) :: non_neg_integer()
  def block_update_count(block_hash), do: Repo.one(block_update_count_query(block_hash))

  @doc false
  def block_update_count_query(block_hash),
    do: from(update in PriceUpdate, where: update.block_hash == ^block_hash, select: count())

  defp cast_hashes(%{hash: hash, feeder_hash: feeder_hash} = update) do
    {:ok, hash} = Hash.Full.cast(hash)
    {:ok, feeder_hash} = Hash.Address.cast(feeder_hash)
    %{update | hash: hash, feeder_hash: feeder_hash}
  end

  defp consensus_updates_query do
    from(update in PriceUpdate,
      as: :update,
      inner_join: block in Block,
      on: block.hash == update.block_hash and block.consensus == true,
      order_by: [desc: update.block_number, desc: update.index]
    )
  end

  defp filter_feeder(query, nil), do: query
  defp filter_feeder(query, feeder), do: where(query, [update], update.feeder_hash == ^feeder)

  defp filter_market(query, nil), do: query

  defp filter_market(query, market_id) do
    where(
      query,
      [update],
      exists(
        from(observation in PriceObservation,
          where: observation.update_hash == parent_as(:update).hash and observation.market_id == ^market_id
        )
      )
    )
  end

  defp page_updates(query, %PagingOptions{key: {block_number, index}}) do
    where(
      query,
      [update: update],
      update.block_number <= ^block_number and
        (update.block_number < ^block_number or update.index < ^index)
    )
  end

  defp page_updates(query, _paging_options), do: query

  defp page_observations(query, %PagingOptions{key: {block_number, index}}) do
    where(
      query,
      [observation, update],
      observation.block_number <= ^block_number and
        (observation.block_number < ^block_number or update.index < ^index)
    )
  end

  defp page_observations(query, _paging_options), do: query

  defp page_size(%PagingOptions{page_size: page_size}) when is_integer(page_size) and page_size > 0, do: page_size
  defp page_size(_paging_options), do: @default_page_size

  defp preload_observations(query) do
    preload(query, observations: ^from(observation in PriceObservation, order_by: observation.market_id))
  end
end

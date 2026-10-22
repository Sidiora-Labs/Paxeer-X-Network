defmodule Explorer.Chain.PaxeerX.PriceFeedTest do
  use Explorer.DataCase

  alias Explorer.Chain.Block
  alias Explorer.Chain.PaxeerX.{PriceFeed, PriceFeedScannedBlock, PriceMarket, PriceObservation, PriceUpdate}
  alias Explorer.PagingOptions
  alias Explorer.PaxeerX.CosmosRest

  @fixtures Path.expand("../../../support/fixture/paxeer_x/price_feed", __DIR__)
  @feeder "0xb97e177fe6e701db2cf85c1ba1307688166318cc"

  setup do
    body = @fixtures |> Path.join("block_txs.json") |> File.read!() |> Jason.decode!()
    results = @fixtures |> Path.join("block_results.json") |> File.read!() |> Jason.decode!()
    raw = body["block"]["data"]["txs"]

    {:ok, submissions} = CosmosRest.decode_block_txs(%{"txs" => body["txs"], "raw" => raw})
    {:ok, [update]} = CosmosRest.decode_price_updates(submissions, results["txs_results"], length(raw))

    %{update: update}
  end

  describe "replace_block_updates/2" do
    test "stores the recorded submission with its observations and marks the height scanned", %{update: update} do
      block = insert(:block)

      assert {:ok, 1} = PriceFeed.replace_block_updates(block, [update])

      assert [%PriceUpdate{} = stored] = PriceFeed.block_updates(block.number)
      assert to_string(stored.hash) == update.hash
      assert to_string(stored.feeder_hash) == @feeder
      assert stored.feeder_bech32 == "pax1h9lpwllxuuqakt8ctsd6zvrk3qtxxxxvyfs9jc"
      assert stored.block_hash == block.hash
      assert stored.status == 1
      assert stored.observation_count == 18
      assert Decimal.equal?(stored.gas_wanted, 200_000)
      assert stored.timestamp == block.timestamp
      assert length(stored.observations) == 18

      assert %PriceObservation{market_id: 0, sequence: 63_784, source_timestamp_ms: 1_792_636_042_680} =
               first = hd(stored.observations)

      assert Decimal.equal?(first.index_price, Decimal.new("82830900000000000000000"))
      assert first.block_number == block.number
      assert Repo.get(PriceFeedScannedBlock, block.number)
    end

    test "rescanning a height replaces what it held", %{update: update} do
      block = insert(:block)
      {:ok, 1} = PriceFeed.replace_block_updates(block, [update])

      assert {:ok, 0} = PriceFeed.replace_block_updates(block, [])
      assert PriceFeed.block_updates(block.number) == []
      assert Repo.aggregate(PriceObservation, :count) == 0
      assert Repo.aggregate(PriceFeedScannedBlock, :count) == 1
    end

    test "deleting the block removes its submissions and observations", %{update: update} do
      block = insert(:block)
      {:ok, 1} = PriceFeed.replace_block_updates(block, [update])

      Repo.delete_all(from(b in Block, where: b.hash == ^block.hash))

      assert Repo.aggregate(PriceUpdate, :count) == 0
      assert Repo.aggregate(PriceObservation, :count) == 0
    end
  end

  describe "reads" do
    test "hide submissions of non-consensus blocks", %{update: update} do
      block = insert(:block, consensus: false)
      {:ok, 1} = PriceFeed.replace_block_updates(block, [update])

      assert PriceFeed.list_updates(%PagingOptions{page_size: 10}) == []
      assert PriceFeed.update_by_hash(update.hash) == nil
    end

    test "page newest first and filter by feeder and market", %{update: update} do
      [older, newer] = Enum.map([10, 11], &insert(:block, number: &1))
      second_hash = "0x" <> String.duplicate("ab", 32)

      {:ok, 1} = PriceFeed.replace_block_updates(older, [update])

      {:ok, 1} =
        PriceFeed.replace_block_updates(newer, [
          %{update | hash: second_hash, observations: [hd(update.observations)]}
        ])

      assert [first, second] = PriceFeed.list_updates(%PagingOptions{page_size: 10})
      assert {first.block_number, second.block_number} == {11, 10}

      assert [only] = PriceFeed.list_updates(%PagingOptions{page_size: 10, key: {11, 0}})
      assert only.block_number == 10

      assert [_, _] = PriceFeed.list_updates(%PagingOptions{page_size: 10}, feeder: cast_address(@feeder))
      assert [] = PriceFeed.list_updates(%PagingOptions{page_size: 10}, feeder: cast_address(String.duplicate("0", 40)))
      assert [%{block_number: 10}] = PriceFeed.list_updates(%PagingOptions{page_size: 10}, market_id: 17)

      assert [%{block_number: 11}, %{block_number: 10}] = PriceFeed.market_updates(0, %PagingOptions{page_size: 10})
      assert [%{block_number: 10}] = PriceFeed.market_updates(0, %PagingOptions{page_size: 10, key: {11, 0}})

      assert %PriceUpdate{block_number: 11} = PriceFeed.update_by_hash(second_hash)
      assert PriceFeed.update_by_hash("not a hash") == nil
      assert [%{block_number: 11}, %{block_number: 10}] = PriceFeed.updates_in_blocks([10, 11])
    end

    test "latest_per_market pairs every market with its newest observation", %{update: update} do
      [older, newer] = Enum.map([20, 21], &insert(:block, number: &1))
      {:ok, 1} = PriceFeed.replace_block_updates(older, [update])

      {:ok, 1} =
        PriceFeed.replace_block_updates(newer, [
          %{update | hash: "0x" <> String.duplicate("cd", 32), observations: [hd(update.observations)]}
        ])

      assert 3 ==
               PriceFeed.upsert_markets([
                 %{market_id: 0, symbol: "BTC", chainflow_symbol: "BTC"},
                 %{market_id: 17, symbol: "BNB", chainflow_symbol: "BNB"},
                 %{market_id: 18, symbol: "NEW", chainflow_symbol: "NEW"}
               ])

      assert [
               {%PriceMarket{market_id: 0}, %PriceObservation{block_number: 21, update: %PriceUpdate{}}},
               {%PriceMarket{market_id: 17}, %PriceObservation{block_number: 20}},
               {%PriceMarket{market_id: 18}, nil}
             ] = PriceFeed.latest_per_market()

      assert %DateTime{} = PriceFeed.markets_refreshed_at()
      assert PriceFeed.unknown_market_ids([0, 1, 17, 1]) == [1]
    end
  end

  describe "query shape" do
    test "latest_per_market reads each market's newest observations through the market index" do
      plan = explain(PriceFeed.latest_per_market_query())

      assert plan =~
               ~r/Index Scan using "?paxeer_x_price_observations_market_id_block_number\w*"? on paxeer_x_price_observations/

      assert plan =~ ~r/Index Cond: \(market_id = \w+\.market_id\)/
      assert plan =~ "Limit"
      assert length(Regex.scan(~r/on paxeer_x_price_observations/, plan)) == 1
      refute plan =~ "Seq Scan on paxeer_x_price_observations"
    end

    test "market_updates pages by a range on the market index" do
      plan = explain(PriceFeed.market_updates_query(0, %PagingOptions{page_size: 10, key: {11, 0}}))

      assert plan =~
               ~r/Index Scan using "?paxeer_x_price_observations_market_id_block_number\w*"? on paxeer_x_price_observations/

      assert plan =~ ~r/Index Cond: \(\(market_id = 0\) AND \(block_number <= '?11'?(::bigint)?\)\)/
      refute plan =~ "Seq Scan on paxeer_x_price_observations"
    end

    test "latest_per_market picks the newest block and the higher index within it on a seeded history", %{
      update: update
    } do
      observation = hd(update.observations)

      for number <- 1..40 do
        block = insert(:block, number: number)

        updates =
          for index <- 0..1 do
            %{
              update
              | hash: "0x" <> String.pad_leading(Integer.to_string(number * 2 + index, 16), 64, "0"),
                index: index,
                observations: [%{observation | sequence: number * 2 + index}]
            }
          end

        {:ok, 2} = PriceFeed.replace_block_updates(block, updates)
      end

      Repo.update_all(from(b in Block, where: b.number == 40), set: [consensus: false])
      PriceFeed.upsert_markets([%{market_id: 0, symbol: "BTC", chainflow_symbol: "BTC"}])

      assert [{%PriceMarket{market_id: 0}, %PriceObservation{block_number: 39, sequence: 79, update: %{index: 1}}}] =
               PriceFeed.latest_per_market()

      assert [%{block_number: 38, update: %{index: 1}}, %{block_number: 38, update: %{index: 0}}] =
               PriceFeed.market_updates(0, %PagingOptions{page_size: 2, key: {39, 0}})
    end
  end

  describe "mark_unscannable/3" do
    test "records a distinct state that catch-up skips and a later scan clears", %{update: update} do
      Enum.each([30, 31], &insert(:block, number: &1))

      assert :ok = PriceFeed.mark_unscannable(30, "unavailable", "{:height_unavailable, \"node\"}")
      assert :ok = PriceFeed.mark_unscannable(31, "failed", "{:unreachable, \"node\"}")

      assert %PriceFeedScannedBlock{status: "unavailable"} = Repo.get(PriceFeedScannedBlock, 30)

      assert %PriceFeedScannedBlock{status: "failed", error: "{:unreachable, \"node\"}"} =
               Repo.get(PriceFeedScannedBlock, 31)

      assert {:ok, []} = PriceFeed.stream_unscanned_block_numbers([], &[&1 | &2], 30)

      {:ok, 1} = PriceFeed.replace_block_updates(Repo.get_by!(Block, number: 30), [update])
      assert %PriceFeedScannedBlock{status: "scanned", error: nil} = Repo.get(PriceFeedScannedBlock, 30)
    end
  end

  describe "stream_unscanned_block_numbers/3" do
    test "selects consensus heights at or above the start that are not scanned yet", %{update: update} do
      Enum.each([4, 5, 7], &insert(:block, number: &1))
      insert(:block, number: 6, consensus: false)
      {:ok, 1} = PriceFeed.replace_block_updates(Repo.get_by!(Block, number: 7), [update])

      assert {:ok, numbers} = PriceFeed.stream_unscanned_block_numbers([], &[&1 | &2], 5)
      assert Enum.reverse(numbers) == [5]
    end
  end

  defp explain(query) do
    Repo.query!("SET LOCAL enable_seqscan = off")
    Repo.query!("SET LOCAL enable_bitmapscan = off")
    Ecto.Adapters.SQL.explain(Repo, :all, query)
  end

  defp cast_address(hex) do
    {:ok, hash} = Explorer.Chain.Hash.Address.cast(if String.starts_with?(hex, "0x"), do: hex, else: "0x" <> hex)
    hash
  end
end

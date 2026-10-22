defmodule Indexer.Fetcher.PaxeerX.PriceFeedMaintenanceTest do
  use Explorer.DataCase

  alias Explorer.Chain.PaxeerX.{PriceFeed, PriceFeedScannedBlock, PriceObservation, PriceUpdate}
  alias Explorer.PaxeerX.CosmosRest
  alias Indexer.Fetcher.PaxeerX.PriceFeedMaintenance

  @fixtures Path.expand("../../../../../explorer/test/support/fixture/paxeer_x/price_feed", __DIR__)

  setup do
    body = @fixtures |> Path.join("block_txs.json") |> File.read!() |> Jason.decode!()
    results = @fixtures |> Path.join("block_results.json") |> File.read!() |> Jason.decode!()
    raw = body["block"]["data"]["txs"]

    {:ok, submissions} = CosmosRest.decode_block_txs(%{"txs" => body["txs"], "raw" => raw})
    {:ok, [update]} = CosmosRest.decode_price_updates(submissions, results["txs_results"], length(raw))

    now = DateTime.utc_now()

    for {number, age_days} <- [{5, 10}, {6, 9}, {7, 2}, {8, 1}] do
      block = insert(:block, number: number, timestamp: DateTime.add(now, -age_days * 86_400, :second))
      hash = "0x" <> String.pad_leading(Integer.to_string(number, 16), 64, "0")
      {:ok, 1} = PriceFeed.replace_block_updates(block, [%{update | hash: hash}])
    end

    insert(:block, number: 9)

    :ok
  end

  test "without a retention period a pass compacts the scanned heights and keeps every submission" do
    assert %{scanned_through: 8, expired: 0} = PriceFeedMaintenance.run_once(5, nil)

    assert Repo.aggregate(PriceFeedScannedBlock, :count) == 0
    assert Repo.aggregate(PriceUpdate, :count) == 4
    assert Repo.aggregate(PriceObservation, :count) == 4 * 18
    assert {:ok, [9]} = PriceFeed.stream_unscanned_block_numbers([], &[&1 | &2], 5)
  end

  test "with a retention period a pass deletes the older submissions with their observations" do
    assert %{scanned_through: 8, expired: 2} = PriceFeedMaintenance.run_once(5, 5)

    assert Repo.all(from(u in PriceUpdate, order_by: u.block_number, select: u.block_number)) == [7, 8]
    assert Repo.aggregate(PriceObservation, :count) == 2 * 18
    assert %{expired: 0} = PriceFeedMaintenance.run_once(5, 5)
  end
end

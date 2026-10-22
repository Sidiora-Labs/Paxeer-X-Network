defmodule Indexer.Fetcher.PaxeerX.PriceUpdatesTest do
  use Explorer.DataCase

  import Mox

  alias Explorer.Chain.Block
  alias Explorer.Chain.PaxeerX.{PriceFeed, PriceFeedScannedBlock, PriceMarket, PriceUpdate}
  alias Indexer.Fetcher.PaxeerX.PriceUpdates

  @moduletag :capture_log

  @fixtures Path.expand("../../../../../explorer/test/support/fixture/paxeer_x/price_feed", __DIR__)
  @precompile "0x0000000000000000000000000000000000001020"
  @get_market_count "0xfd69f3c2"
  @get_market "0xeb44fdd3"

  setup :verify_on_exit!

  setup do
    rest = Bypass.open()
    rpc = Bypass.open()

    state = %{
      json_rpc_named_arguments: [transport: EthereumJSONRPC.Mox, transport_options: [], variant: EthereumJSONRPC.Geth],
      cosmos_rest_urls: ["http://localhost:#{rest.port}"],
      cometbft_rpc_urls: ["http://localhost:#{rpc.port}"],
      start_block: 5,
      precompile: @precompile,
      markets_refresh_seconds: 300
    }

    %{rest: rest, rpc: rpc, state: state}
  end

  describe "init/3" do
    test "buffers the unscanned consensus heights at or above the start block", %{state: state} do
      Enum.each([4, 5, 6], &insert(:block, number: &1))
      insert(:block, number: 7, consensus: false)
      Repo.insert_all(PriceFeedScannedBlock, [%{block_number: 6}])

      assert PriceUpdates.init([], &[&1 | &2], state) == [5]
    end
  end

  describe "run/2" do
    test "stores the recorded submission and reads the market list", %{rest: rest, rpc: rpc, state: state} do
      block = insert(:block, number: 10)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/10", 200, "block_txs.json")
      serve(rpc, "/block_results", 200, "block_results.json")
      expect_markets()

      assert PriceUpdates.run([10], state) == :ok

      assert [%PriceUpdate{status: 1, observation_count: 18} = update] = PriceFeed.block_updates(10)
      assert update.block_hash == block.hash
      assert to_string(update.hash) == "0xa57f246774eee2ec5ac6ac506b279d22682af01a62812d9aa642447751b0fadf"
      assert Repo.get(PriceFeedScannedBlock, 10)

      markets = Repo.all(from(market in PriceMarket, order_by: market.market_id))
      assert length(markets) == 18
      assert %PriceMarket{market_id: 0, symbol: "BTC", chainflow_symbol: "BTC"} = hd(markets)
      assert %PriceMarket{market_id: 17, symbol: "BNB"} = List.last(markets)
    end

    test "marks a block without transactions scanned without asking for block results", %{rest: rest, state: state} do
      insert(:block, number: 11)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/11", 400, "block_txs_empty.json")

      assert PriceUpdates.run([11], state) == :ok
      assert PriceFeed.block_updates(11) == []
      assert Repo.get(PriceFeedScannedBlock, 11)
    end

    test "a rescan after a reorg replaces the stored submission", %{rest: rest, rpc: rpc, state: state} do
      old = insert(:block, number: 12)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/12", 200, "block_txs.json")
      serve(rpc, "/block_results", 200, "block_results.json")
      expect_markets()
      assert PriceUpdates.run([12], state) == :ok

      Repo.update_all(from(b in Block, where: b.hash == ^old.hash), set: [consensus: false])
      new = insert(:block, number: 12)

      assert PriceUpdates.run([12], state) == :ok
      assert [%PriceUpdate{block_hash: block_hash}] = Repo.all(PriceUpdate)
      assert block_hash == new.hash
    end

    test "retries a height whose node read fails and stores nothing", %{state: state} do
      insert(:block, number: 13)
      down = Bypass.open()
      Bypass.down(down)

      assert PriceUpdates.run([13], %{state | cosmos_rest_urls: ["http://localhost:#{down.port}"]}) == {:retry, [13]}
      assert Repo.get(PriceFeedScannedBlock, 13) == nil
    end

    test "skips a height with no consensus block", %{state: state} do
      assert PriceUpdates.run([14], state) == :ok
      assert Repo.get(PriceFeedScannedBlock, 14) == nil
    end
  end

  defp serve(bypass, path, status, fixture) do
    Bypass.expect(bypass, "GET", path, fn conn ->
      Plug.Conn.resp(conn, status, File.read!(Path.join(@fixtures, fixture)))
    end)
  end

  defp expect_markets do
    recorded = @fixtures |> Path.join("markets.json") |> File.read!() |> Jason.decode!()

    stub(EthereumJSONRPC.Mox, :json_rpc, fn requests, _options ->
      {:ok,
       Enum.map(requests, fn
         %{id: id, method: "eth_call", params: [%{to: @precompile, data: @get_market_count}, "latest"]} ->
           %{id: id, jsonrpc: "2.0", result: recorded["getMarketCount"]}

         %{id: id, method: "eth_call", params: [%{to: @precompile, data: @get_market <> argument}, "latest"]} ->
           {market_id, ""} = Integer.parse(argument, 16)
           %{id: id, jsonrpc: "2.0", result: Map.fetch!(recorded["getMarket"], to_string(market_id))}
       end)}
    end)
  end
end

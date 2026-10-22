defmodule Indexer.Fetcher.PaxeerX.PriceUpdatesTest do
  use Explorer.DataCase

  import Mox

  alias Explorer.Chain.Block
  alias Explorer.Chain.Events.Subscriber
  alias Explorer.Chain.PaxeerX.{PriceFeed, PriceFeedScannedBlock, PriceMarket, PriceUpdate}
  alias Indexer.BufferedTask
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
      markets_refresh_seconds: 300,
      markets_attempted_at: PriceUpdates.refresh_clock()
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

      assert :ok = PriceUpdates.run([10], state)

      assert [%PriceUpdate{status: 1, observation_count: 18} = update] = PriceFeed.block_updates(10)
      assert update.block_hash == block.hash
      assert to_string(update.hash) == "0xa57f246774eee2ec5ac6ac506b279d22682af01a62812d9aa642447751b0fadf"
      assert Repo.get(PriceFeedScannedBlock, 10)

      markets = Repo.all(from(market in PriceMarket, order_by: market.market_id))
      assert length(markets) == 18
      assert %PriceMarket{market_id: 0, symbol: "BTC", chainflow_symbol: "BTC"} = hd(markets)
      assert %PriceMarket{market_id: 17, symbol: "BNB"} = List.last(markets)
    end

    test "publishes the height once its submissions are committed", %{rest: rest, rpc: rpc, state: state} do
      insert(:block, number: 10)
      insert(:block, number: 11)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/10", 200, "block_txs.json")
      serve(rest, "/cosmos/tx/v1beta1/txs/block/11", 400, "block_txs_empty.json")
      serve(rpc, "/block_results", 200, "block_results.json")
      expect_markets()
      Subscriber.to(:paxeer_x_price_updates, :realtime)

      assert :ok = PriceUpdates.run([10, 11], state)

      assert_receive {:chain_event, :paxeer_x_price_updates, :realtime, [10]}
      assert [%PriceUpdate{}] = PriceFeed.block_updates(10)
      refute_receive {:chain_event, :paxeer_x_price_updates, :realtime, _}
    end

    test "marks a block without transactions scanned without asking for block results", %{rest: rest, state: state} do
      insert(:block, number: 11)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/11", 400, "block_txs_empty.json")

      assert :ok = PriceUpdates.run([11], state)
      assert PriceFeed.block_updates(11) == []
      assert Repo.get(PriceFeedScannedBlock, 11)
    end

    test "a rescan after a reorg replaces the stored submission", %{rest: rest, rpc: rpc, state: state} do
      old = insert(:block, number: 12)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/12", 200, "block_txs.json")
      serve(rpc, "/block_results", 200, "block_results.json")
      expect_markets()
      assert :ok = PriceUpdates.run([12], state)

      Repo.update_all(from(b in Block, where: b.hash == ^old.hash), set: [consensus: false])
      new = insert(:block, number: 12)

      assert :ok = PriceUpdates.run([12], state)
      assert [%PriceUpdate{block_hash: block_hash}] = Repo.all(PriceUpdate)
      assert block_hash == new.hash
    end

    test "buffers a height whose node read fails again once its delay has passed and stores nothing", %{
      state: state
    } do
      insert(:block, number: 13)
      Process.register(self(), PriceUpdates)
      before = System.monotonic_time(:millisecond)

      assert :ok = PriceUpdates.run([13], down(state))
      refute_received {:buffer, [{13, _, _, _}], _}
      assert_receive {:buffer, [{13, 1, not_before, 0}], false}, 2_000
      assert not_before >= before + PriceUpdates.backoff_ms(1)
      assert System.monotonic_time(:millisecond) >= not_before
      assert Repo.get(PriceFeedScannedBlock, 13) == nil

      before = System.monotonic_time(:millisecond)
      assert :ok = PriceUpdates.run([{13, 1, nil, 0}], down(state))
      assert_receive {:buffer, [{13, 2, later, 0}], false}, 3_000
      assert later >= before + PriceUpdates.backoff_ms(2)
      assert PriceUpdates.backoff_ms(2) > PriceUpdates.backoff_ms(1)
      assert System.monotonic_time(:millisecond) >= later
      assert PriceUpdates.backoff_ms(20) == 300_000
    end

    test "buffers a height that is not due yet again for its remaining delay without scanning it", %{state: state} do
      insert(:block, number: 13)
      Process.register(self(), PriceUpdates)
      entry = {13, 3, System.monotonic_time(:millisecond) + 300, 2}

      assert :ok = PriceUpdates.run([entry], state)
      refute_received {:buffer, [{13, _, _, _}], _}
      assert_receive {:buffer, [^entry], false}, 1_000
      assert System.monotonic_time(:millisecond) >= elem(entry, 2)
      assert Repo.get(PriceFeedScannedBlock, 13) == nil
    end

    test "records a height as failed once its definitive failures run out and catch-up scans it again", %{
      rest: rest,
      state: state
    } do
      insert(:block, number: 13)
      Process.register(self(), PriceUpdates)

      Bypass.expect(rest, "GET", "/cosmos/tx/v1beta1/txs/block/13", fn conn ->
        Plug.Conn.resp(conn, 503, ~s({"code":13,"message":"internal"}))
      end)

      assert :ok = PriceUpdates.run([{13, 0, nil, 0}], state)
      assert_receive {:buffer, [{13, 1, _, 1}], false}, 2_000
      assert Repo.get(PriceFeedScannedBlock, 13) == nil

      assert :ok = PriceUpdates.run([{13, 9, nil, 9}], state)
      assert %PriceFeedScannedBlock{status: "failed", error: error} = Repo.get(PriceFeedScannedBlock, 13)
      assert error =~ "http_status"
      assert PriceUpdates.init([], &[&1 | &2], state) == [13]

      Repo.insert_all(PriceFeedScannedBlock, Enum.map(5..12, &%{block_number: &1, status: "scanned"}))
      assert PriceFeed.compact_scanned_blocks(5, 100) == 13
      assert PriceUpdates.init([], &[&1 | &2], state) == [13]
    end

    test "keeps retrying an unreachable height without counting it toward the failure cap", %{state: state} do
      insert(:block, number: 13)
      Process.register(self(), PriceUpdates)

      assert :ok = PriceUpdates.run([{13, 0, nil, 9}], down(state))
      assert_receive {:buffer, [{13, 1, _, 9}], false}, 2_000
      assert Repo.get(PriceFeedScannedBlock, 13) == nil
    end

    test "records a height the nodes no longer serve as unavailable without retrying", %{rest: rest, state: state} do
      insert(:block, number: 15)
      Process.register(self(), PriceUpdates)

      Bypass.expect(rest, "GET", "/cosmos/tx/v1beta1/txs/block/15", fn conn ->
        Plug.Conn.resp(conn, 500, ~s({"code":2,"message":"height 15 is not available, lowest height is 60"}))
      end)

      assert :ok = PriceUpdates.run([15], state)
      assert %PriceFeedScannedBlock{status: "unavailable"} = Repo.get(PriceFeedScannedBlock, 15)
      assert PriceFeed.block_updates(15) == []
      refute_receive {:buffer, [{15, _, _, _}], _}, 1_500
      assert PriceUpdates.init([], &[&1 | &2], state) == []
    end

    test "keeps retrying a height above the nodes' head without counting it toward the failure cap", %{
      rest: rest,
      state: state
    } do
      insert(:block, number: 16)
      Process.register(self(), PriceUpdates)

      Bypass.expect(rest, "GET", "/cosmos/tx/v1beta1/txs/block/16", fn conn ->
        Plug.Conn.resp(
          conn,
          500,
          ~s({"code":2,"message":"height 16 must be less than or equal to the current blockchain height 9"})
        )
      end)

      assert :ok = PriceUpdates.run([{16, 0, nil, 9}], state)
      assert_receive {:buffer, [{16, 1, _, 9}], false}, 2_000
      assert Repo.get(PriceFeedScannedBlock, 16) == nil
    end

    test "spaces market refreshes by the refresh interval when a market stays unknown", %{
      rest: rest,
      rpc: rpc,
      state: state
    } do
      insert(:block, number: 10)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/10", 200, "block_txs.json")
      serve(rpc, "/block_results", 200, "block_results.json")
      expect_markets(4, 17)

      assert :ok = PriceUpdates.run([10], state)
      attempted_at = :atomics.get(state.markets_attempted_at, 1)
      assert PriceFeed.unknown_market_ids([0, 17]) == [17]

      assert :ok = PriceUpdates.run([10], state)
      assert :atomics.get(state.markets_attempted_at, 1) == attempted_at

      :atomics.put(state.markets_attempted_at, 1, attempted_at - 301_000)
      assert :ok = PriceUpdates.run([10], state)
      assert :atomics.get(state.markets_attempted_at, 1) > attempted_at - 301_000
      assert PriceFeed.unknown_market_ids([17]) == [17]
    end

    test "a batch that finishes after a failed refresh does not reopen the refresh interval", %{
      rest: rest,
      rpc: rpc,
      state: state
    } do
      insert(:block, number: 10)
      insert(:block, number: 11)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/10", 200, "block_txs.json")
      serve(rest, "/cosmos/tx/v1beta1/txs/block/11", 400, "block_txs_empty.json")
      serve(rpc, "/block_results", 200, "block_results.json")

      expect(EthereumJSONRPC.Mox, :json_rpc, 1, fn requests, _options ->
        {:ok, Enum.map(requests, &%{id: &1.id, jsonrpc: "2.0", error: %{code: -32_000, message: "unavailable"}})}
      end)

      assert :ok = PriceUpdates.run([10], state)
      assert :ok = PriceUpdates.run([11], state)
      assert PriceFeed.markets_refreshed_at() == nil

      assert :ok = PriceUpdates.run([10], state)
      assert PriceFeed.block_updates(10) != []

      :atomics.sub(state.markets_attempted_at, 1, 301_000)
      expect_markets(2, nil)
      assert :ok = PriceUpdates.run([10], state)
      assert PriceFeed.markets_refreshed_at()
    end

    test "concurrent batches with submissions refresh the markets once per interval", %{
      rest: rest,
      rpc: rpc,
      state: state
    } do
      insert(:block, number: 10)
      serve(rest, "/cosmos/tx/v1beta1/txs/block/10", 200, "block_txs.json")
      serve(rpc, "/block_results", 200, "block_results.json")
      expect_markets(2, nil)

      assert [:ok, :ok] =
               [10, 10]
               |> Enum.map(fn number -> Task.async(fn -> PriceUpdates.run([number], state) end) end)
               |> Task.await_many(10_000)

      assert PriceFeed.unknown_market_ids([0, 17]) == []
    end

    test "skips a height with no consensus block", %{state: state} do
      assert :ok = PriceUpdates.run([14], state)
      assert Repo.get(PriceFeedScannedBlock, 14) == nil
    end
  end

  describe "under BufferedTask" do
    test "failing batches stay dormant until their retries are due", %{rest: rest, state: state} do
      numbers = Enum.to_list(20..31)
      # every scan reads the one node twice before it gives up
      reads = 2 * length(numbers)
      Enum.each(numbers, &insert(:block, number: &1))
      scans = :counters.new(1, [])

      Bypass.stub(rest, "GET", "/cosmos/tx/v1beta1/txs/block/:height", fn conn ->
        :counters.add(scans, 1, 1)
        Plug.Conn.resp(conn, 503, ~s({"code":13,"message":"internal"}))
      end)

      :erlang.trace_pattern({PriceUpdates, :run, 2}, true, [:call_count])
      on_exit(fn -> :erlang.trace_pattern({PriceUpdates, :run, 2}, false, [:call_count]) end)

      start_supervised!({Task.Supervisor, name: PriceUpdatesTestTaskSupervisor})

      start_supervised!(
        {BufferedTask,
         [
           {PriceUpdates,
            state: %{state | start_block: 20},
            poll: false,
            flush_interval: 50,
            max_batch_size: 1,
            max_concurrency: length(numbers),
            task_supervisor: PriceUpdatesTestTaskSupervisor},
           [name: PriceUpdates]
         ]}
      )

      wait_until(fn -> :counters.get(scans, 1) == reads end, 900)
      Process.sleep(300)

      assert :counters.get(scans, 1) == reads
      assert {:call_count, 12} = :erlang.trace_info({PriceUpdates, :run, 2}, :call_count)
      assert Enum.all?(numbers, &(Repo.get(PriceFeedScannedBlock, &1) == nil))

      wait_until(fn -> :counters.get(scans, 1) == 2 * reads end, 2_000)
      assert {:call_count, 24} = :erlang.trace_info({PriceUpdates, :run, 2}, :call_count)
      Process.sleep(200)
      assert :counters.get(scans, 1) == 2 * reads
    end
  end

  defp wait_until(done?, timeout) do
    cond do
      done?.() ->
        :ok

      timeout <= 0 ->
        flunk("condition not met in time")

      true ->
        Process.sleep(10)
        wait_until(done?, timeout - 10)
    end
  end

  defp serve(bypass, path, status, fixture) do
    Bypass.expect(bypass, "GET", path, fn conn ->
      Plug.Conn.resp(conn, status, File.read!(Path.join(@fixtures, fixture)))
    end)
  end

  defp down(state) do
    down = Bypass.open()
    Bypass.down(down)
    %{state | cosmos_rest_urls: ["http://localhost:#{down.port}"]}
  end

  defp expect_markets(calls, failing_market_id) do
    recorded = @fixtures |> Path.join("markets.json") |> File.read!() |> Jason.decode!()

    expect(EthereumJSONRPC.Mox, :json_rpc, calls, fn requests, _options ->
      {:ok,
       Enum.map(requests, fn
         %{id: id, method: "eth_call", params: [%{to: @precompile, data: @get_market_count}, "latest"]} ->
           %{id: id, jsonrpc: "2.0", result: recorded["getMarketCount"]}

         %{id: id, method: "eth_call", params: [%{to: @precompile, data: @get_market <> argument}, "latest"]} ->
           case Integer.parse(argument, 16) do
             {^failing_market_id, ""} ->
               %{id: id, jsonrpc: "2.0", error: %{code: -32_000, message: "execution reverted"}}

             {market_id, ""} ->
               %{id: id, jsonrpc: "2.0", result: Map.fetch!(recorded["getMarket"], to_string(market_id))}
           end
       end)}
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

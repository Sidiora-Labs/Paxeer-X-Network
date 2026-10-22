defmodule Explorer.PaxeerX.CosmosRestTest do
  use ExUnit.Case, async: false

  alias Explorer.PaxeerX.CosmosRest

  @fixtures Path.expand("../../support/fixture/paxeer_x/price_feed", __DIR__)
  @height 1_000
  @empty_height 1_001
  @hash "0xa57f246774eee2ec5ac6ac506b279d22682af01a62812d9aa642447751b0fadf"

  setup do
    rest = Bypass.open()
    rpc = Bypass.open()
    %{rest: rest, rpc: rpc, rest_url: url(rest), rpc_url: url(rpc)}
  end

  test "decodes the recorded block with one price submission", context do
    serve_block(context, @height, 200, "block_txs.json", "block_results.json")

    assert {:ok, [update]} = CosmosRest.block_price_updates([context.rest_url], [context.rpc_url], @height)

    assert %{
             hash: @hash,
             index: 0,
             feeder_bech32: "pax1h9lpwllxuuqakt8ctsd6zvrk3qtxxxxvyfs9jc",
             feeder_hash: "0xb97e177fe6e701db2cf85c1ba1307688166318cc",
             status: 1,
             gas_used: 0,
             gas_wanted: 200_000,
             observations: observations
           } = update

    assert length(observations) == 18
    assert Enum.map(observations, & &1.market_id) == Enum.to_list(0..17)

    assert hd(observations) == %{
             market_id: 0,
             sequence: 63_784,
             index_price: 82_830_900_000_000_000_000_000,
             mark_price: 82_830_900_000_000_000_000_000,
             source_timestamp_ms: 1_792_636_042_680
           }

    assert List.last(observations).index_price == 740_540_000_000_000_000_000
  end

  test "reads the recorded 400 for a block without transactions as empty and skips block results", context do
    Bypass.expect_once(context.rest, "GET", "/cosmos/tx/v1beta1/txs/block/#{@empty_height}", fn conn ->
      Plug.Conn.resp(conn, 400, fixture("block_txs_empty.json"))
    end)

    assert {:ok, []} = CosmosRest.block_price_updates([context.rest_url], [context.rpc_url], @empty_height)
  end

  test "reads a recorded empty block results document as no transactions", context do
    Bypass.expect_once(context.rpc, "GET", "/block_results", fn conn ->
      Plug.Conn.resp(conn, 200, fixture("block_results_empty.json"))
    end)

    assert {:ok, []} = CosmosRest.fetch_block_results([context.rpc_url], @empty_height)
  end

  test "marks a submission with a nonzero code failed and keeps its gas used", context do
    results = Jason.decode!(fixture("block_results.json"))
    failed = update_in(results, ["txs_results", Access.at(0)], &Map.merge(&1, %{"code" => 5, "gas_used" => "41"}))

    serve_block(context, @height, 200, "block_txs.json", Jason.encode!(failed))

    assert {:ok, [%{status: 0, gas_used: 41}]} =
             CosmosRest.block_price_updates([context.rest_url], [context.rpc_url], @height)
  end

  test "refuses execution results that do not line up with the block's transactions", context do
    results = Jason.decode!(fixture("block_results.json"))
    serve_block(context, @height, 200, "block_txs.json", Jason.encode!(%{results | "txs_results" => []}))

    assert {:error, {:malformed, _}} = CosmosRest.block_price_updates([context.rest_url], [context.rpc_url], @height)
  end

  test "fails over to the next URL when the first is down", context do
    down = Bypass.open()
    Bypass.down(down)
    serve_block(context, @height, 200, "block_txs.json", "block_results.json")

    assert {:ok, [%{hash: @hash}]} =
             CosmosRest.block_price_updates([url(down), context.rest_url], [url(down), context.rpc_url], @height)
  end

  test "goes round the URLs a bounded number of times and returns the last typed error", context do
    calls = :counters.new(1, [])

    Bypass.expect(context.rest, "GET", "/cosmos/tx/v1beta1/txs/block/#{@height}", fn conn ->
      :counters.add(calls, 1, 1)
      Plug.Conn.resp(conn, 503, ~s({"code":14,"message":"unavailable"}))
    end)

    rest_url = context.rest_url

    assert {:error, {:http_status, 503, ^rest_url}} =
             CosmosRest.block_price_updates([rest_url], [context.rpc_url], @height)

    assert :counters.get(calls, 1) == 2
    assert {:error, :no_urls} = CosmosRest.block_price_updates([], [context.rpc_url], @height)
  end

  test "refuses a submission whose feeder is not a pax account" do
    body = Jason.decode!(fixture("block_txs.json"))

    broken =
      update_in(body, ["txs", Access.at(0), "body", "messages", Access.at(0), "feeder"], fn _ -> "cosmos1xyz" end)

    assert {:error, {:malformed, "feeder is not a pax account"}} =
             CosmosRest.decode_block_txs(%{"txs" => broken["txs"], "raw" => broken["block"]["data"]["txs"]})
  end

  defp serve_block(context, height, status, txs_fixture, results) do
    Bypass.expect(context.rest, "GET", "/cosmos/tx/v1beta1/txs/block/#{height}", fn conn ->
      Plug.Conn.resp(conn, status, fixture(txs_fixture))
    end)

    Bypass.expect(context.rpc, "GET", "/block_results", fn conn ->
      conn = Plug.Conn.fetch_query_params(conn)
      assert conn.query_params["height"] == to_string(height)
      body = if String.ends_with?(results, ".json"), do: fixture(results), else: results
      Plug.Conn.resp(conn, 200, body)
    end)
  end

  defp fixture(name), do: File.read!(Path.join(@fixtures, name))

  defp url(bypass), do: "http://localhost:#{bypass.port}"
end

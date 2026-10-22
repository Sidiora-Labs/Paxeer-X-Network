defmodule BlockScoutWeb.API.V2.PaxeerX.FeedControllerTest do
  use BlockScoutWeb.ConnCase, async: false

  alias Explorer.Chain.Address
  alias Explorer.Chain.PaxeerX.PriceFeed

  @feeder "0xb97e177fe6e701db2cf85c1ba1307688166318cc"
  @feeder_bech32 "pax1h9lpwllxuuqakt8ctsd6zvrk3qtxxxxvyfs9jc"
  @other_feeder "0x00000000000000000000000000000000000000aa"
  @supervisor Indexer.Fetcher.PaxeerX.PriceUpdates.Supervisor

  setup do
    Supervisor.terminate_child(Explorer.Supervisor, Explorer.Chain.Cache.Transactions.child_id())
    Supervisor.restart_child(Explorer.Supervisor, Explorer.Chain.Cache.Transactions.child_id())

    PriceFeed.upsert_markets([
      %{market_id: 0, symbol: "BTC-USD", chainflow_symbol: "BTCUSDT"},
      %{market_id: 1, symbol: "ETH-USD", chainflow_symbol: "ETHUSDT"},
      %{market_id: 2, symbol: "SOL-USD", chainflow_symbol: "SOLUSDT"}
    ])

    :ok
  end

  describe "GET /api/v2/paxeer-x/feed/markets" do
    test "lists every market with its latest consensus observation", %{conn: conn} do
      older = insert(:block, number: 10)
      newer = insert(:block, number: 11)
      store(older, [submission(0, observations: [observation(0, 100), observation(1, 200)])])
      [latest] = store(newer, [submission(0, observations: [observation(0, 101)])])

      response = json_response(get(conn, "/api/v2/paxeer-x/feed/markets"), 200)

      assert response["chain_head"] == 11

      assert [btc, eth, sol] = response["items"]
      assert %{"market_id" => 0, "symbol" => "BTC-USD", "chainflow_symbol" => "BTCUSDT"} = btc

      assert btc["latest"] == %{
               "update_hash" => latest,
               "block_number" => 11,
               "timestamp" => iso(newer.timestamp),
               "index_price" => price(101),
               "mark_price" => price(102),
               "source_timestamp_ms" => 1_792_636_042_101,
               "sequence" => 101
             }

      assert eth["latest"]["block_number"] == 10
      assert eth["latest"]["index_price"] == price(200)
      assert sol["latest"] == nil
    end
  end

  describe "GET /api/v2/paxeer-x/feed/updates" do
    test "renders a submission as a transaction item", %{conn: conn} do
      block = insert(:block, number: 20)
      insert(:block, number: 25)
      [hash] = store(block, [submission(3, observations: [observation(2, 7), observation(0, 5)], gas_used: 1234)])

      response = json_response(get(conn, "/api/v2/paxeer-x/feed/updates"), 200)

      assert response["next_page_params"] == nil

      assert [item] = response["items"]

      assert item == %{
               "hash" => hash,
               "block_number" => 20,
               "block_hash" => to_string(block.hash),
               "timestamp" => iso(block.timestamp),
               "index" => 3,
               "from" => %{"hash" => checksum(@feeder), "is_contract" => false},
               "to" => nil,
               "value" => "0",
               "fee" => %{"type" => "actual", "value" => "0"},
               "gas_used" => "1234",
               "gas_limit" => "200000",
               "status" => "ok",
               "result" => "success",
               "method" => "submitPrices",
               "transaction_types" => ["price_update"],
               "type" => 127,
               "confirmations" => 6,
               "price_update" => %{
                 "feeder_bech32" => @feeder_bech32,
                 "observation_count" => 2,
                 "observations" => [
                   %{
                     "market_id" => 0,
                     "symbol" => "BTC-USD",
                     "index_price" => price(5),
                     "mark_price" => price(6),
                     "source_timestamp_ms" => 1_792_636_042_005,
                     "sequence" => 5
                   },
                   %{
                     "market_id" => 2,
                     "symbol" => "SOL-USD",
                     "index_price" => price(7),
                     "mark_price" => price(8),
                     "source_timestamp_ms" => 1_792_636_042_007,
                     "sequence" => 7
                   }
                 ]
               }
             }
    end

    test "reports a failed submission as an error", %{conn: conn} do
      block = insert(:block, number: 20)
      store(block, [submission(0, status: 0)])

      assert [%{"status" => "error", "result" => "failed"}] =
               json_response(get(conn, "/api/v2/paxeer-x/feed/updates"), 200)["items"]
    end

    test "pages newest first across blocks", %{conn: conn} do
      for number <- 1..26 do
        store(insert(:block, number: number), [submission(0), submission(1)])
      end

      first = json_response(get(conn, "/api/v2/paxeer-x/feed/updates"), 200)

      assert length(first["items"]) == 50
      assert %{"block_number" => 26, "index" => 1} = hd(first["items"])
      assert %{"block_number" => 2, "index" => 0} = List.last(first["items"])
      assert %{"block_number" => 2, "index" => 0, "items_count" => 50} = first["next_page_params"]

      second = json_response(get(conn, "/api/v2/paxeer-x/feed/updates", first["next_page_params"]), 200)

      assert Enum.map(second["items"], &{&1["block_number"], &1["index"]}) == [{1, 1}, {1, 0}]
      assert second["next_page_params"] == nil
    end

    test "filters by feeder and by market", %{conn: conn} do
      block = insert(:block, number: 30)

      [mine, _theirs] =
        store(block, [
          submission(0, observations: [observation(0, 1)]),
          submission(1, feeder: @other_feeder, observations: [observation(1, 1)])
        ])

      by_feeder = json_response(get(conn, "/api/v2/paxeer-x/feed/updates", %{"feeder" => checksum(@feeder)}), 200)
      assert Enum.map(by_feeder["items"], & &1["hash"]) == [mine]

      by_market = json_response(get(conn, "/api/v2/paxeer-x/feed/updates", %{"market_id" => "0"}), 200)
      assert Enum.map(by_market["items"], & &1["hash"]) == [mine]
    end

    test "hides the submissions of a non-consensus block", %{conn: conn} do
      block = insert(:block, number: 40, consensus: false)
      store(block, [submission(0)])

      assert json_response(get(conn, "/api/v2/paxeer-x/feed/updates"), 200)["items"] == []
    end
  end

  describe "GET /api/v2/paxeer-x/feed/updates/:hash" do
    test "returns the submission", %{conn: conn} do
      block = insert(:block, number: 50)
      [hash] = store(block, [submission(2)])

      response = json_response(get(conn, "/api/v2/paxeer-x/feed/updates/#{hash}"), 200)

      assert response["hash"] == hash
      assert response["index"] == 2
      assert response["price_update"]["observation_count"] == 1
    end

    test "answers 404 for an unknown hash", %{conn: conn} do
      request = get(conn, "/api/v2/paxeer-x/feed/updates/0x" <> String.duplicate("ab", 32))

      assert %{"message" => "Not found"} = json_response(request, 404)
    end

    test "answers 422 for a malformed hash", %{conn: conn} do
      assert json_response(get(conn, "/api/v2/paxeer-x/feed/updates/0x1234"), 422)
    end
  end

  describe "GET /api/v2/paxeer-x/feed/markets/:market_id/updates" do
    test "lists one market's observations newest first with paging", %{conn: conn} do
      for number <- 1..51 do
        store(insert(:block, number: number), [submission(0, observations: [observation(1, number)])])
      end

      first = json_response(get(conn, "/api/v2/paxeer-x/feed/markets/1/updates"), 200)

      assert length(first["items"]) == 50
      assert first["symbol"] == "ETH-USD"

      assert %{"block_number" => 51, "index_price" => index_price, "sequence" => 51, "update_hash" => "0x" <> _} =
               hd(first["items"])

      assert index_price == price(51)
      assert %{"block_number" => 2, "index" => 0} = first["next_page_params"]

      second = json_response(get(conn, "/api/v2/paxeer-x/feed/markets/1/updates", first["next_page_params"]), 200)

      assert [%{"block_number" => 1}] = second["items"]
      assert second["next_page_params"] == nil

      assert json_response(get(conn, "/api/v2/paxeer-x/feed/markets/0/updates"), 200)["items"] == []
    end

    test "carries a null symbol for an unknown market", %{conn: conn} do
      assert %{"symbol" => nil, "items" => [], "next_page_params" => nil} =
               json_response(get(conn, "/api/v2/paxeer-x/feed/markets/9/updates"), 200)
    end
  end

  describe "GET /api/v2/paxeer-x/capabilities" do
    test "with no scanned block carries price_feed only while the price feed is enabled and the indexer runs", %{
      conn: conn
    } do
      previous_fetcher = Application.get_env(:indexer, @supervisor)
      previous_indexer = Application.get_env(:indexer, Indexer.Supervisor)

      on_exit(fn ->
        Application.put_env(:indexer, @supervisor, previous_fetcher || [])
        Application.put_env(:indexer, Indexer.Supervisor, previous_indexer || [])
      end)

      Application.put_env(:indexer, Indexer.Supervisor, enabled: true)
      Application.put_env(:indexer, @supervisor, disabled?: true)
      refute Map.has_key?(json_response(get(conn, "/api/v2/paxeer-x/capabilities"), 200), "price_feed")

      Application.put_env(:indexer, @supervisor, disabled?: false)
      assert %{"price_feed" => true} = json_response(get(conn, "/api/v2/paxeer-x/capabilities"), 200)

      Application.put_env(:indexer, Indexer.Supervisor, enabled: false)
      refute Map.has_key?(json_response(get(conn, "/api/v2/paxeer-x/capabilities"), 200), "price_feed")
    end

    test "carries price_feed on a node without an indexer once another indexer has scanned a block", %{conn: conn} do
      previous_fetcher = Application.get_env(:indexer, @supervisor)
      previous_indexer = Application.get_env(:indexer, Indexer.Supervisor)

      on_exit(fn ->
        Application.put_env(:indexer, @supervisor, previous_fetcher || [])
        Application.put_env(:indexer, Indexer.Supervisor, previous_indexer || [])
      end)

      Application.put_env(:indexer, Indexer.Supervisor, enabled: false)
      Application.put_env(:indexer, @supervisor, disabled?: true)
      refute Map.has_key?(json_response(get(conn, "/api/v2/paxeer-x/capabilities"), 200), "price_feed")

      store(insert(:block, number: 30), [])
      assert %{"price_feed" => true} = json_response(get(conn, "/api/v2/paxeer-x/capabilities"), 200)
    end
  end

  describe "GET /api/v2/transactions" do
    test "interleaves submissions with EVM transactions and pages across a block boundary", %{conn: conn} do
      older = insert(:block, number: 100)
      newer = insert(:block, number: 101)

      older_transactions = 2 |> insert_list(:transaction) |> with_block(older)
      newer_transactions = 48 |> insert_list(:transaction) |> with_block(newer)

      [older_first, older_second] = store(older, [submission(0), submission(1)])
      [newer_update] = store(newer, [submission(0)])

      first = json_response(get(conn, "/api/v2/transactions"), 200)

      expected_first =
        [newer_update] ++
          (newer_transactions |> Enum.reverse() |> Enum.map(&to_string(&1.hash))) ++ [older_second]

      assert Enum.map(first["items"], & &1["hash"]) == expected_first
      assert hd(first["items"])["transaction_types"] == ["price_update"]
      assert %{"block_number" => 100, "index" => 1_000_001, "items_count" => 50} = first["next_page_params"]

      second = json_response(get(conn, "/api/v2/transactions", first["next_page_params"]), 200)

      assert Enum.map(second["items"], & &1["hash"]) ==
               [older_first | older_transactions |> Enum.reverse() |> Enum.map(&to_string(&1.hash))]

      assert second["next_page_params"] == nil
    end

    test "type price_update lists only submissions", %{conn: conn} do
      block = insert(:block, number: 100)
      :transaction |> insert() |> with_block(block)
      [hash] = store(block, [submission(0)])

      response = json_response(get(conn, "/api/v2/transactions", %{"type" => "price_update"}), 200)

      assert Enum.map(response["items"], & &1["hash"]) == [hash]
    end

    test "pending transactions carry no submission", %{conn: conn} do
      store(insert(:block, number: 100), [submission(0)])
      pending = insert(:transaction)

      response = json_response(get(conn, "/api/v2/transactions", %{"filter" => "pending"}), 200)

      assert Enum.map(response["items"], & &1["hash"]) == [to_string(pending.hash)]
    end
  end

  describe "GET /api/v2/transactions/:hash" do
    test "returns a submission by its hash", %{conn: conn} do
      block = insert(:block, number: 60)
      [hash] = store(block, [submission(4)])

      response = json_response(get(conn, "/api/v2/transactions/#{hash}"), 200)

      assert %{"hash" => ^hash, "type" => 127, "method" => "submitPrices", "index" => 4} = response
    end

    test "answers 404 when neither a transaction nor a submission has the hash", %{conn: conn} do
      request = get(conn, "/api/v2/transactions/0x" <> String.duplicate("cd", 32))

      assert %{"message" => "Not found"} = json_response(request, 404)
    end
  end

  describe "GET /api/v2/blocks/:id/transactions" do
    test "lists a block's submissions after its EVM transactions and pages into them", %{conn: conn} do
      block = insert(:block, number: 70)
      transactions = 50 |> insert_list(:transaction) |> with_block(block)
      [first_update, second_update] = store(block, [submission(0), submission(1)])

      first = json_response(get(conn, "/api/v2/blocks/70/transactions"), 200)

      assert Enum.map(first["items"], & &1["hash"]) == Enum.map(transactions, &to_string(&1.hash))
      assert %{"block_number" => 70, "index" => 49} = first["next_page_params"]

      second = json_response(get(conn, "/api/v2/blocks/70/transactions", first["next_page_params"]), 200)

      assert Enum.map(second["items"], & &1["hash"]) == [first_update, second_update]
      assert second["next_page_params"] == nil

      only_updates = json_response(get(conn, "/api/v2/blocks/70/transactions", %{"type" => "price_update"}), 200)
      assert Enum.map(only_updates["items"], & &1["hash"]) == [first_update, second_update]
    end

    test "lists only EVM transactions for a block without submissions", %{conn: conn} do
      block = insert(:block, number: 71)
      transactions = 2 |> insert_list(:transaction) |> with_block(block)
      store(insert(:block, number: 72), [submission(0)])

      response = json_response(get(conn, "/api/v2/blocks/71/transactions"), 200)

      assert Enum.map(response["items"], & &1["hash"]) == Enum.map(transactions, &to_string(&1.hash))
    end
  end

  describe "GET /api/v2/blocks/:id" do
    test "counts the block's submissions", %{conn: conn} do
      store(insert(:block, number: 80), [submission(0), submission(1)])
      insert(:block, number: 81)

      assert %{"price_update_count" => 2} = json_response(get(conn, "/api/v2/blocks/80"), 200)
      assert %{"price_update_count" => 0} = json_response(get(conn, "/api/v2/blocks/81"), 200)
    end
  end

  describe "GET /api/v2/search" do
    test "finds a submission by its hash", %{conn: conn} do
      block = insert(:block, number: 90)
      [hash] = store(block, [submission(0)])

      response = json_response(get(conn, "/api/v2/search", %{"q" => hash}), 200)

      assert %{"type" => "price_update", "hash" => ^hash, "block_number" => 90} =
               Enum.find(response["items"], &(&1["type"] == "price_update"))
    end

    test "finds a submission by its upper-case hash without 0x", %{conn: conn} do
      block = insert(:block, number: 91)
      [hash] = store(block, [submission(0)])
      "0x" <> hex = hash

      response = json_response(get(conn, "/api/v2/search", %{"q" => String.upcase(hex)}), 200)

      assert %{"type" => "price_update", "hash" => ^hash, "block_number" => 91} =
               Enum.find(response["items"], &(&1["type"] == "price_update"))

      for query <- ["0x" <> String.upcase(hex), "0X" <> String.upcase(hex), String.upcase(hex)] do
        quick = json_response(get(conn, "/api/v2/search/quick", %{"q" => query}), 200)

        assert %{"type" => "price_update", "hash" => ^hash} = Enum.find(quick, &(&1["type"] == "price_update"))
      end
    end
  end

  describe "GET /api/v2/search/check-redirect" do
    test "redirects to a submission by its hash, with or without 0x and in any case", %{conn: conn} do
      block = insert(:block, number: 92)
      [hash] = store(block, [submission(0)])
      "0x" <> hex = hash

      for query <- [hash, "0X" <> hex, String.upcase(hex)] do
        assert %{"redirect" => true, "type" => "price_update", "parameter" => ^hash} =
                 json_response(get(conn, "/api/v2/search/check-redirect", %{"q" => query}), 200)
      end
    end

    test "does not redirect for an unknown 64 hex character query", %{conn: conn} do
      assert %{"redirect" => false} =
               json_response(
                 get(conn, "/api/v2/search/check-redirect", %{"q" => String.duplicate("EF", 32)}),
                 200
               )
    end
  end

  defp store(block, submissions) do
    {:ok, _count} = PriceFeed.replace_block_updates(block, submissions)
    Enum.map(submissions, & &1.hash)
  end

  defp submission(index, options \\ []) do
    %{
      hash: "0x" <> Base.encode16(:crypto.strong_rand_bytes(32), case: :lower),
      index: index,
      feeder_bech32: @feeder_bech32,
      feeder_hash: Keyword.get(options, :feeder, @feeder),
      status: Keyword.get(options, :status, 1),
      gas_used: Keyword.get(options, :gas_used, 100_000),
      gas_wanted: 200_000,
      observations: Keyword.get(options, :observations, [observation(0, 1)])
    }
  end

  defp observation(market_id, value) do
    %{
      market_id: market_id,
      sequence: value,
      index_price: value * 1_000_000_000_000_000_000,
      mark_price: (value + 1) * 1_000_000_000_000_000_000,
      source_timestamp_ms: 1_792_636_042_000 + value
    }
  end

  defp price(value), do: Integer.to_string(value * 1_000_000_000_000_000_000)

  defp checksum(hash) do
    {:ok, address_hash} = Explorer.Chain.Hash.Address.cast(hash)
    Address.checksum(address_hash)
  end

  defp iso(timestamp), do: DateTime.to_iso8601(timestamp)
end

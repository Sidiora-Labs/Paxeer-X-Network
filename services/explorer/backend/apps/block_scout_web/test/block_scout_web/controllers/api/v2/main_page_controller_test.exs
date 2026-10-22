defmodule BlockScoutWeb.API.V2.MainPageControllerTest do
  use BlockScoutWeb.ConnCase

  alias Explorer.Account.{Identity, WatchlistAddress}
  alias Explorer.Chain.{Address, Block, Transaction}
  alias Explorer.Chain.PaxeerX.PriceFeed
  alias Explorer.Repo

  import Explorer.Chain, only: [hash_to_lower_case_string: 1]

  setup do
    Supervisor.terminate_child(Explorer.Supervisor, Explorer.Chain.Cache.Blocks.child_id())
    Supervisor.restart_child(Explorer.Supervisor, Explorer.Chain.Cache.Blocks.child_id())
    Supervisor.terminate_child(Explorer.Supervisor, Explorer.Chain.Cache.Transactions.child_id())
    Supervisor.restart_child(Explorer.Supervisor, Explorer.Chain.Cache.Transactions.child_id())

    :ok
  end

  describe "/main-page/blocks" do
    test "get empty list when no blocks", %{conn: conn} do
      request = get(conn, "/api/v2/main-page/blocks")
      assert [] = json_response(request, 200)
    end

    test "get last 4 blocks", %{conn: conn} do
      blocks = insert_list(10, :block) |> Enum.take(-4) |> Enum.reverse()

      request = get(conn, "/api/v2/main-page/blocks")
      assert response = json_response(request, 200)
      assert Enum.count(response) == 4

      for i <- 0..3 do
        compare_item(Enum.at(blocks, i), Enum.at(response, i))
      end
    end
  end

  describe "/main-page/transactions" do
    test "get empty list when no transactions", %{conn: conn} do
      request = get(conn, "/api/v2/main-page/transactions")
      assert [] = json_response(request, 200)
    end

    test "get last 6 transactions", %{conn: conn} do
      transactions = insert_list(10, :transaction) |> with_block() |> Enum.take(-6) |> Enum.reverse()

      request = get(conn, "/api/v2/main-page/transactions")
      assert response = json_response(request, 200)
      assert Enum.count(response) == 6

      for i <- 0..5 do
        compare_item(Enum.at(transactions, i), Enum.at(response, i))
      end
    end

    test "interleaves the newest submissions with the newest EVM transactions", %{conn: conn} do
      older = insert(:block, number: 100)
      newer = insert(:block, number: 101)

      older_transactions = 2 |> insert_list(:transaction) |> with_block(older)
      newer_transactions = 3 |> insert_list(:transaction) |> with_block(newer)

      [older_update] = store(older, [submission(0)])
      [newer_update] = store(newer, [submission(0)])

      response = json_response(get(conn, "/api/v2/main-page/transactions"), 200)

      expected =
        [newer_update] ++
          (newer_transactions |> Enum.reverse() |> Enum.map(&to_string(&1.hash))) ++
          [older_update, to_string(List.last(older_transactions).hash)]

      assert Enum.map(response, & &1["hash"]) == expected
      assert %{"transaction_types" => ["price_update"], "type" => 127} = hd(response)
    end
  end

  describe "/main-page/transactions/watchlist" do
    test "unauthorized", %{conn: conn} do
      request = get(conn, "/api/v2/main-page/transactions/watchlist")
      assert %{"message" => "Unauthorized"} = json_response(request, 401)
    end

    test "get last 6 transactions", %{conn: conn} do
      insert_list(10, :transaction) |> with_block()

      auth = build(:auth)
      {:ok, user} = Identity.find_or_create(auth)

      conn = Plug.Test.init_test_session(conn, current_user: user)

      address_1 = insert(:address)

      watchlist_address_1 =
        Repo.account_repo().insert!(%WatchlistAddress{
          name: "wallet_1",
          watchlist_id: user.watchlist_id,
          address_hash: address_1.hash,
          address_hash_hash: hash_to_lower_case_string(address_1.hash),
          watch_coin_input: true,
          watch_coin_output: true,
          watch_erc_20_input: true,
          watch_erc_20_output: true,
          watch_erc_721_input: true,
          watch_erc_721_output: true,
          watch_erc_1155_input: true,
          watch_erc_1155_output: true,
          notify_email: true
        })

      address_2 = insert(:address)

      watchlist_address_2 =
        Repo.account_repo().insert!(%WatchlistAddress{
          name: "wallet_2",
          watchlist_id: user.watchlist_id,
          address_hash: address_2.hash,
          address_hash_hash: hash_to_lower_case_string(address_2.hash),
          watch_coin_input: true,
          watch_coin_output: true,
          watch_erc_20_input: true,
          watch_erc_20_output: true,
          watch_erc_721_input: true,
          watch_erc_721_output: true,
          watch_erc_1155_input: true,
          watch_erc_1155_output: true,
          notify_email: true
        })

      transactions_1 = insert_list(2, :transaction, from_address: address_1) |> with_block()
      transactions_2 = insert_list(1, :transaction, from_address: address_2, to_address: address_1) |> with_block()
      transactions_3 = insert_list(3, :transaction, to_address: address_2) |> with_block()
      transactions = (transactions_1 ++ transactions_2 ++ transactions_3) |> Enum.reverse()

      request = get(conn, "/api/v2/main-page/transactions/watchlist")
      assert response = json_response(request, 200)
      assert Enum.count(response) == 6

      for i <- 0..5 do
        compare_item(Enum.at(transactions, i), Enum.at(response, i), %{
          address_1.hash => watchlist_address_1.name,
          address_2.hash => watchlist_address_2.name
        })
      end
    end
  end

  describe "/main-page/indexing-status" do
    test "get indexing status", %{conn: conn} do
      request = get(conn, "/api/v2/main-page/indexing-status")
      assert request = json_response(request, 200)

      assert Map.has_key?(request, "finished_indexing_blocks")
      assert Map.has_key?(request, "finished_indexing")
      assert Map.has_key?(request, "indexed_blocks_ratio")
      assert Map.has_key?(request, "indexed_internal_transactions_ratio")
    end
  end

  defp store(block, submissions) do
    {:ok, _count} = PriceFeed.replace_block_updates(block, submissions)
    Enum.map(submissions, & &1.hash)
  end

  defp submission(index) do
    %{
      hash: "0x" <> Base.encode16(:crypto.strong_rand_bytes(32), case: :lower),
      index: index,
      feeder_bech32: "pax1h9lpwllxuuqakt8ctsd6zvrk3qtxxxxvyfs9jc",
      feeder_hash: "0xb97e177fe6e701db2cf85c1ba1307688166318cc",
      status: 1,
      gas_used: 100_000,
      gas_wanted: 200_000,
      observations: [
        %{
          market_id: 0,
          sequence: 1,
          index_price: 1_000_000_000_000_000_000,
          mark_price: 2_000_000_000_000_000_000,
          source_timestamp_ms: 1_792_636_042_001
        }
      ]
    }
  end

  defp compare_item(%Block{} = block, json) do
    assert to_string(block.hash) == json["hash"]
    assert block.number == json["height"]
  end

  defp compare_item(%Transaction{} = transaction, json) do
    assert to_string(transaction.hash) == json["hash"]
    assert transaction.block_number == json["block_number"]
    assert to_string(transaction.value.value) == json["value"]
    assert Address.checksum(transaction.from_address_hash) == json["from"]["hash"]
    assert Address.checksum(transaction.to_address_hash) == json["to"]["hash"]
  end

  defp compare_item(%Transaction{} = transaction, json, wl_names) do
    assert to_string(transaction.hash) == json["hash"]
    assert transaction.block_number == json["block_number"]
    assert to_string(transaction.value.value) == json["value"]
    assert Address.checksum(transaction.from_address_hash) == json["from"]["hash"]
    assert Address.checksum(transaction.to_address_hash) == json["to"]["hash"]

    assert json["to"]["watchlist_names"] ==
             if(wl_names[transaction.to_address_hash],
               do: [
                 %{
                   "display_name" => wl_names[transaction.to_address_hash],
                   "label" => wl_names[transaction.to_address_hash]
                 }
               ],
               else: []
             )

    assert json["from"]["watchlist_names"] ==
             if(wl_names[transaction.from_address_hash],
               do: [
                 %{
                   "display_name" => wl_names[transaction.from_address_hash],
                   "label" => wl_names[transaction.from_address_hash]
                 }
               ],
               else: []
             )
  end
end

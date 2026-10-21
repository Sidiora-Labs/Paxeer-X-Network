defmodule BlockScoutWeb.API.V2.PaxeerX.UnifiedAccountControllerTest do
  use BlockScoutWeb.ConnCase

  alias Explorer.Chain.Address
  alias Explorer.Chain.PaxeerX.{AccountBinding, CustodyEvent, UnifiedAccount}
  alias Explorer.Repo

  @pax_address "pax1005qwm6w5jj26zq8tsjs3eyp6my5d5fthrlqk3"
  @kernel_key "3f1a9b0c5d2e4f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8"
  @asset_id "0x" <> String.duplicate("0", 63) <> "1"
  @cursor_salt "paxeer-x-native-account-v2"
  @refused "malformed, wrong account, changed binding or reorganized snapshot"

  describe "GET /api/v2/addresses/:address_hash_param/unified" do
    test "rejects a malformed address", %{conn: conn} do
      for value <- ["0xdeadbeef", "0x" <> String.duplicate("g", 40)] do
        assert %{"message" => "Invalid parameter(s)"} ==
                 json_response(get(conn, "/api/v2/addresses/#{value}/unified"), 422)
      end
    end

    test "knows only the evm spelling while the address has never been bound", %{conn: conn} do
      address = insert(:address, fetched_coin_balance: 0)
      checksummed = Address.checksum(address.hash)
      response = json_response(get(conn, path(address)), 200)

      assert response["identities"] == %{"evm" => checksummed, "pax" => nil, "did" => nil}
      assert response["activity"] == []
      assert response["activity_total"] == 0
      assert response["next_page_params"] == nil
      assert response["page_number"] == 1
      assert [%{"total" => "0"}] = response["balances"]
    end

    test "answers with the native identities and three PAX of a bound account", %{conn: conn} do
      address = insert(:address, fetched_coin_balance: 3_000_000_000_000_000_000)
      insert_binding(address, did: "did:lx:a")
      response = json_response(get(conn, path(address)), 200)

      assert response["identities"] == %{
               "evm" => Address.checksum(address.hash),
               "pax" => @pax_address,
               "did" => "did:lx:a"
             }

      assert response["balances"] == [
               %{
                 "asset" => %{
                   "id" => "native",
                   "denom" => Explorer.coin(),
                   "symbol" => Explorer.coin(),
                   "decimals" => 18
                 },
                 "total" => "3000000000000000000"
               }
             ]
    end

    test "forgets the identities the newest log unbound", %{conn: conn} do
      address = insert(:address)
      block = insert(:block, number: 300)
      transaction = :transaction |> insert() |> with_block(block)

      insert_binding(address, block: block, transaction: transaction, log_index: 0, bound: true)
      insert_binding(address, block: block, transaction: transaction, log_index: 1, bound: false)
      response = json_response(get(conn, path(address)), 200)

      assert response["identities"] == %{
               "evm" => Address.checksum(address.hash),
               "pax" => nil,
               "did" => nil
             }
    end

    test "reads bank and ERC-20 balances without adding custody or kernel balances", %{conn: conn} do
      address = insert(:address, fetched_coin_balance: 1_000)
      insert_binding(address)
      token = insert(:token, type: "ERC-20", symbol: "TOKEN", decimals: 6)
      collectible = insert(:token, type: "ERC-721")

      for {asset, value} <- [{token, 250}, {collectible, 1}] do
        insert(:address_current_token_balance,
          address: address,
          token_contract_address_hash: asset.contract_address_hash,
          token_type: asset.type,
          value: value
        )
      end

      block = insert(:block, number: 400)
      transaction = :transaction |> insert() |> with_block(block)

      insert_custody_event(block, transaction,
        log_index: 0,
        kind: :custody_deposit,
        direction: :deposit,
        amount: Decimal.new(700),
        address_hash: to_string(address.hash),
        account: account_hash()
      )

      insert_custody_event(block, transaction,
        log_index: 1,
        kind: :custody_release,
        direction: :withdrawal,
        amount: Decimal.new(200),
        address_hash: to_string(address.hash),
        account: nil
      )

      response = json_response(get(conn, path(address)), 200)
      items = Map.new(response["balances"], &{&1["asset"]["id"], &1})
      assert Enum.sort(Map.keys(items)) == Enum.sort(["native", Address.checksum(token.contract_address_hash)])

      assert items["native"] == %{
               "asset" => %{
                 "id" => "native",
                 "denom" => Explorer.coin(),
                 "symbol" => Explorer.coin(),
                 "decimals" => 18
               },
               "total" => "1000"
             }

      assert items[Address.checksum(token.contract_address_hash)] == %{
               "asset" => %{
                 "id" => Address.checksum(token.contract_address_hash),
                 "denom" => "TOKEN",
                 "symbol" => "TOKEN",
                 "decimals" => 6
               },
               "total" => "250"
             }
    end

    test "returns native transactions without token transfer duplicates or kernel events", %{conn: conn} do
      address = insert(:address)
      counterparty = insert(:address)
      insert_binding(address)
      first_block = insert(:block, number: 100)
      second_block = insert(:block, number: 200)
      third_block = insert(:block, number: 300)

      sent =
        :transaction
        |> insert(from_address: address, to_address: counterparty, value: 42)
        |> with_block(first_block, status: :ok)

      received =
        :transaction
        |> insert(from_address: counterparty, to_address: address, value: 0)
        |> with_block(second_block, status: :error)

      insert(:token_transfer,
        transaction: received,
        block: second_block,
        block_number: second_block.number,
        from_address: address
      )

      event_transaction = :transaction |> insert() |> with_block(third_block)

      insert_custody_event(third_block, event_transaction,
        log_index: 7,
        kind: :custody_deposit,
        direction: :deposit,
        amount: Decimal.new(42),
        address_hash: to_string(address.hash),
        account: account_hash()
      )

      response = json_response(get(conn, path(address)), 200)
      items = response["activity"]
      assert Enum.map(items, & &1["kind"]) == ["transaction", "transaction"]
      assert Enum.map(items, & &1["hash"]) == [to_string(received.hash), to_string(sent.hash)]
      assert Enum.map(items, & &1["block_number"]) == [200, 100]
      assert Enum.map(items, & &1["status"]) == ["committed", "committed"]
      assert Enum.map(items, & &1["result"]) == ["error", "ok"]
      assert Enum.map(items, & &1["amount"]) == ["0", "42"]
      assert Enum.map(items, & &1["counterparty"]) == List.duplicate(Address.checksum(counterparty.hash), 2)
      assert Enum.map(items, & &1["asset"]["id"]) == ["native", "native"]

      assert Enum.map(items, & &1["timestamp"]) ==
               Enum.map([second_block, first_block], &DateTime.to_iso8601(&1.timestamp))

      assert response["activity_total"] == 2
      assert Enum.all?(items, &(not Map.has_key?(&1, "side")))
    end

    test "excludes a kernel event even when it leaves its asset, amount and counterparty null", %{conn: conn} do
      address = insert(:address, fetched_coin_balance: 100)
      block = insert(:block, number: 500)
      transaction = :transaction |> insert() |> with_block(block)

      insert_custody_event(block, transaction,
        log_index: 0,
        kind: :custody_release,
        direction: :withdrawal,
        amount: nil,
        asset_id: nil,
        address_hash: to_string(address.hash),
        account: nil
      )

      response = json_response(get(conn, path(address)), 200)
      assert response["activity"] == []
      assert [%{"total" => "100"}] = response["balances"]
    end

    test "omits anchors, kernel receipts and split domain fields", %{conn: conn} do
      address = insert(:address)
      insert_binding(address)
      response = json_response(get(conn, path(address)), 200)

      assert Enum.sort(Map.keys(response)) ==
               Enum.sort(
                 ~w(identities balances activity next_page_params page_cursor first_page_cursor page_number activity_total)
               )

      for key <- ~w(anchors receipts receipt kernel_receipts custody kernel) do
        refute Map.has_key?(response, key)
      end

      refute Map.has_key?(response["identities"], "kernel_account")
      assert Enum.all?(response["balances"], &(not Map.has_key?(&1, "parts")))
      refute function_exported?(UnifiedAccount, :anchors, 3)
      refute function_exported?(UnifiedAccount, :receipts, 3)
      refute function_exported?(UnifiedAccount, :receipt, 2)
    end

    test "pages the activity feed using the account-bound server continuation", %{conn: conn} do
      address = insert(:address)

      for number <- 1..51 do
        block = insert(:block, number: number)
        :transaction |> insert(from_address: address) |> with_block(block)
      end

      response = json_response(get(conn, path(address)), 200)
      assert Enum.map(response["activity"], & &1["block_number"]) == Enum.to_list(51..2)
      assert response["activity_total"] == nil
      second_page = json_response(get(conn, path(address), response["next_page_params"]), 200)
      assert Enum.map(second_page["activity"], & &1["block_number"]) == [1]
      assert second_page["next_page_params"] == nil
      assert second_page["page_number"] == 2

      for params <- [%{"block_number" => "2", "index" => "0"}, %{"items_count" => "50"}] do
        assert json_response(get(conn, path(address), params), 422) == %{
                 "message" => "Invalid activity cursor",
                 "reason" => "use the returned cursor instead of a partial activity key"
               }
      end

      other = insert(:address)
      assert json_response(get(conn, path(other), response["next_page_params"]), 422) == refusal()
    end

    test "keeps transaction index ties across pages and excludes concurrent inserts", %{conn: conn} do
      address = insert(:address)

      for number <- 11..59 do
        block = insert(:block, number: number)
        :transaction |> insert(from_address: address) |> with_block(block)
      end

      block = insert(:block, number: 10)
      lower = :transaction |> insert(from_address: address) |> with_block(block, index: 0)
      higher = :transaction |> insert(from_address: address) |> with_block(block, index: 1)

      insert(:token_transfer,
        transaction: higher,
        block: block,
        block_number: 10,
        from_address: address,
        log_index: 0
      )

      insert_custody_event(block, higher,
        log_index: 0,
        kind: :custody_deposit,
        direction: :deposit,
        address_hash: to_string(address.hash),
        account: nil
      )

      first = json_response(get(conn, path(address)), 200)
      assert length(first["activity"]) == 50
      assert List.last(first["activity"])["hash"] == to_string(higher.hash)
      newer = insert(:block, number: 60)
      :transaction |> insert(from_address: address) |> with_block(newer)
      :transaction |> insert(from_address: address) |> with_block(block, index: 2)
      second = json_response(get(conn, path(address), first["next_page_params"]), 200)
      assert Enum.map(second["activity"], & &1["hash"]) == [to_string(lower.hash)]
      assert second["next_page_params"] == nil

      assert json_response(get(conn, path(address), %{"cursor" => first["page_cursor"]}), 200)["activity"] ==
               first["activity"]

      assert json_response(get(conn, path(address), %{"cursor" => second["first_page_cursor"]}), 200)["activity"] ==
               first["activity"]

      {:ok, state} = Phoenix.Token.verify(@endpoint, @cursor_salt, first["page_cursor"], max_age: 3600)
      assert state.version == 2
      refute Map.has_key?(state, :kernel)

      expired = Phoenix.Token.sign(@endpoint, @cursor_salt, state, signed_at: System.system_time(:second) - 3601)

      assert json_response(get(conn, path(address), %{"cursor" => expired}), 422) == %{
               "message" => "Invalid activity cursor",
               "reason" => "expired"
             }

      assert json_response(get(conn, path(address), %{"cursor" => "malformed"}), 422) == refusal()

      assert json_response(get(conn, path(address), %{"cursor" => String.duplicate("x", 4097)}), 422) == %{
               "message" => "Invalid activity cursor",
               "reason" => "malformed"
             }
    end

    test "honours a cursor only while its anchor names a block that holds consensus", %{conn: conn} do
      address = insert(:address)

      blocks =
        for number <- 1..51 do
          block = insert(:block, number: number)
          :transaction |> insert(from_address: address) |> with_block(block)
          block
        end

      anchor_block = List.last(blocks)
      first = json_response(get(conn, path(address)), 200)
      cursor = first["next_page_params"]["cursor"]
      {:ok, state} = Phoenix.Token.verify(@endpoint, @cursor_salt, cursor, max_age: 3600)
      assert state.anchor == to_string(anchor_block.hash)
      second = json_response(get(conn, path(address), first["next_page_params"]), 200)
      assert Enum.map(second["activity"], & &1["block_number"]) == [1]

      for anchor <- ["not-a-block-hash", to_string(block_hash())] do
        invalid = Phoenix.Token.sign(@endpoint, @cursor_salt, %{state | anchor: anchor})
        assert json_response(get(conn, path(address), %{"cursor" => invalid}), 422) == refusal()
      end

      Repo.update!(Ecto.Changeset.change(anchor_block, consensus: false))
      assert json_response(get(conn, path(address), first["next_page_params"]), 422) == refusal()
    end

    test "refuses cursors after the DID binding changes and refuses invalid native keys", %{conn: conn} do
      address = insert(:address)
      insert_binding(address)
      block = insert(:block, number: 100)
      :transaction |> insert(from_address: address) |> with_block(block)
      first = json_response(get(conn, path(address)), 200)
      {:ok, state} = Phoenix.Token.verify(@endpoint, @cursor_salt, first["page_cursor"], max_age: 3600)

      for key <- [{-1, 0}, {100, -1}, {100, 0, "transaction"}, "invalid"] do
        invalid = Phoenix.Token.sign(@endpoint, @cursor_salt, %{state | after: key})
        assert json_response(get(conn, path(address), %{"cursor" => invalid}), 422) == refusal()
      end

      insert_binding(address, block: block, log_index: 1, bound: false)
      assert json_response(get(conn, path(address), %{"cursor" => first["page_cursor"]}), 422) == refusal()
    end

    test "ignores bindings and transactions that lose consensus", %{conn: conn} do
      address = insert(:address)
      binding = insert_binding(address)

      Repo.update!(
        Ecto.Changeset.change(Repo.get_by!(Explorer.Chain.Block, hash: binding.block_hash), consensus: false)
      )

      block = insert(:block, number: 100)
      :transaction |> insert(from_address: address) |> with_block(block)
      Repo.update!(Ecto.Changeset.change(block, consensus: false))
      :transaction |> insert(from_address: address)
      response = json_response(get(conn, path(address)), 200)
      assert response["identities"] == %{"evm" => Address.checksum(address.hash), "pax" => nil, "did" => nil}
      assert response["activity"] == []
    end
  end

  defp path(address), do: "/api/v2/addresses/#{Address.checksum(address.hash)}/unified"
  defp refusal, do: %{"message" => "Invalid activity cursor", "reason" => @refused}

  defp insert_binding(address, options \\ []) do
    block = Keyword.get_lazy(options, :block, fn -> insert(:block, number: 10) end)
    transaction = Keyword.get_lazy(options, :transaction, fn -> :transaction |> insert() |> with_block(block) end)

    attributes = %{
      transaction_hash: transaction.hash,
      log_index: Keyword.get(options, :log_index, 0),
      block_hash: block.hash,
      block_number: block.number,
      block_consensus: true,
      evm_address_hash: to_string(address.hash),
      pax_address: @pax_address,
      layerx_did: Keyword.get(options, :did, did()),
      layerx_account: kernel_account(),
      bound: Keyword.get(options, :bound, true),
      nonce: 1
    }

    %AccountBinding{}
    |> AccountBinding.changeset(attributes)
    |> Repo.insert!()
  end

  defp insert_custody_event(block, transaction, fields) do
    attributes =
      Map.merge(
        %{
          transaction_hash: transaction.hash,
          block_hash: block.hash,
          block_number: block.number,
          block_consensus: true,
          asset_id: @asset_id
        },
        Map.new(fields)
      )

    %CustodyEvent{}
    |> CustodyEvent.changeset(attributes)
    |> Repo.insert!()
  end

  defp did, do: "did:layerx:" <> @kernel_key
  defp kernel_account, do: "agent:did:layerx:" <> @kernel_key <> ":main"
  defp account_hash, do: "0x" <> @kernel_key
end

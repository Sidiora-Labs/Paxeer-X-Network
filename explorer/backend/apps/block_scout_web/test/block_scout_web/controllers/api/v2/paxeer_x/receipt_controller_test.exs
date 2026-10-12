defmodule BlockScoutWeb.API.V2.PaxeerX.ReceiptControllerTest do
  use BlockScoutWeb.ConnCase

  alias Explorer.Chain.PaxeerX.Receipt
  alias Explorer.Repo

  describe "GET /api/v2/paxeer-x/receipts" do
    test "answers with an empty list while the kernel has logged no receipt", %{conn: conn} do
      response = json_response(get(conn, "/api/v2/paxeer-x/receipts"), 200)

      assert response == %{"items" => [], "next_page_params" => nil}
    end

    test "answers with the receipts newest id first", %{conn: conn} do
      for index <- 1..3, do: insert_receipt(index)

      response = json_response(get(conn, "/api/v2/paxeer-x/receipts"), 200)

      assert Enum.map(response["items"], & &1["id"]) == [receipt_id(3), receipt_id(2), receipt_id(1)]
      assert response["next_page_params"] == nil

      [latest | _rest] = response["items"]

      assert latest["account"] == account_hash()
      assert latest["status"] == "committed"
      assert latest["block_number"] == 30
    end

    test "places each receipt on the rung its block reaches against the committed height", %{conn: conn} do
      insert_receipt(4)
      insert_kernel_receipt(8)

      response = json_response(get(conn, "/api/v2/paxeer-x/receipts"), 200)

      assert [kernel, evm] = response["items"]

      assert kernel["id"] == receipt_id(8)
      assert kernel["origin"] == "kernel"
      assert kernel["block_number"] == nil
      assert kernel["status"] == "submitted"

      assert evm["id"] == receipt_id(4)
      assert evm["origin"] == "evm"
      assert evm["block_number"] == 40
      assert evm["status"] == "committed"
    end

    test "carries the newest row of a receipt that climbed the verification lattice", %{conn: conn} do
      insert_receipt(5, status: :sequencer_signed)
      insert_receipt(5, status: :state_proven, block_number: 90)

      response = json_response(get(conn, "/api/v2/paxeer-x/receipts"), 200)

      assert [%{"block_number" => 90}] = response["items"]
    end

    test "carries as null the account a receipt log left out", %{conn: conn} do
      insert_receipt(9, account: nil)

      response = json_response(get(conn, "/api/v2/paxeer-x/receipts"), 200)

      assert [%{"account" => nil, "id" => id}] = response["items"]
      assert id == receipt_id(9)
    end

    test "paginates", %{conn: conn} do
      for index <- 100..150, do: insert_receipt(index)

      response = json_response(get(conn, "/api/v2/paxeer-x/receipts"), 200)

      assert Enum.count(response["items"]) == 50
      assert response["next_page_params"] == %{"id" => receipt_id(101), "items_count" => 50}

      second_page = json_response(get(conn, "/api/v2/paxeer-x/receipts", response["next_page_params"]), 200)

      assert Enum.map(second_page["items"], & &1["id"]) == [receipt_id(100)]
      assert second_page["next_page_params"] == nil
    end
  end

  describe "GET /api/v2/paxeer-x/receipts/:id" do
    test "answers 404 while the kernel has logged no receipt", %{conn: conn} do
      assert %{"message" => "Not found"} = json_response(get(conn, "/api/v2/paxeer-x/receipts/#{receipt_id(1)}"), 404)
    end

    test "answers 404 for an id the kernel does not carry", %{conn: conn} do
      insert_receipt(1)

      assert %{"message" => "Not found"} = json_response(get(conn, "/api/v2/paxeer-x/receipts/#{receipt_id(2)}"), 404)
    end

    test "answers 404 for an id that is not a kernel receipt id", %{conn: conn} do
      assert %{"message" => "Not found"} = json_response(get(conn, "/api/v2/paxeer-x/receipts/receipt-1"), 404)
    end

    test "carries as null the account and the payload hash a receipt log left out", %{conn: conn} do
      insert_receipt(9, account: nil, payload_hash: nil)

      response = json_response(get(conn, "/api/v2/paxeer-x/receipts/#{receipt_id(9)}"), 200)

      assert response["account"] == nil
      assert response["payload_hash"] == nil
      assert response["verification_status"] == "sequencer_signed"
    end

    test "answers with one receipt", %{conn: conn} do
      receipt = insert_receipt(7, status: :checkpoint_finalised)

      response = json_response(get(conn, "/api/v2/paxeer-x/receipts/#{receipt_id(7)}"), 200)

      assert response["id"] == receipt_id(7)
      assert response["account"] == account_hash()
      assert response["status"] == "committed"
      assert response["verification_status"] == "checkpoint_finalised"
      assert response["block_number"] == 70
      assert response["transaction_hash"] == to_string(receipt.transaction_hash)
      refute is_nil(response["timestamp"])
    end

    test "answers with one kernel receipt held below the committed rung", %{conn: conn} do
      insert_receipt(2)
      insert_kernel_receipt(6)

      response = json_response(get(conn, "/api/v2/paxeer-x/receipts/#{receipt_id(6)}"), 200)

      assert response["id"] == receipt_id(6)
      assert response["origin"] == "kernel"
      assert response["status"] == "submitted"
      assert response["block_number"] == nil
      assert response["verification"] == "sequencer_verified"
      assert response["verification_status"] == "batch_included"
      assert response["transaction_hash"] == nil
      assert response["timestamp"] == nil
      assert response["provenance"]["kernel"]["batch_number"] == "6"
      assert response["provenance"]["kernel"]["sequence"] == "1"
      assert response["provenance"]["kernel"]["activity_id"] == receipt_id(6)
    end
  end

  defp insert_receipt(index, overrides \\ []) do
    overrides = Map.new(overrides)
    block = insert(:block, number: Map.get(overrides, :block_number, index * 10))
    transaction = :transaction |> insert() |> with_block(block)

    attributes =
      Map.merge(
        %{
          transaction_hash: transaction.hash,
          log_index: 0,
          block_hash: block.hash,
          block_number: block.number,
          block_consensus: true,
          receipt_id: receipt_id(index),
          account: account_hash(),
          payload_hash: to_string(block_hash()),
          status: :sequencer_signed
        },
        Map.drop(overrides, [:block_number])
      )

    %Receipt{}
    |> Receipt.changeset(attributes)
    |> Repo.insert!()
  end

  defp insert_kernel_receipt(index) do
    batch_id = digest("batch-#{index}")
    raw_sha256 = digest("raw-#{index}")

    %Receipt{}
    |> Receipt.changeset(%{
      origin: :kernel,
      receipt_id: receipt_id(index),
      account: account_hash(),
      payload_hash: digest("payload-#{index}"),
      status: :batch_included,
      kernel_batch_number: index,
      kernel_batch_id: batch_id,
      kernel_sequence: 1,
      kernel_activity_id: receipt_id(index),
      kernel_result_code: 0,
      kernel_receipt_sha256: digest("receipt-#{index}"),
      kernel_canonical_sha256: digest("canonical-#{index}"),
      kernel_batch_raw_sha256: raw_sha256,
      kernel_state_root: digest("root-#{index}"),
      kernel_proof: %{
        "batch_id" => String.trim_leading(batch_id, "0x"),
        "header_hex" => "0a0b",
        "signature_hex" => String.duplicate("cd", 64),
        "raw_sha256" => String.trim_leading(raw_sha256, "0x")
      },
      kernel_verification: :sequencer_verified
    })
    |> Repo.insert!()
  end

  defp digest(label), do: "0x" <> Base.encode16(:crypto.hash(:sha256, label), case: :lower)

  defp receipt_id(index), do: "0x" <> Base.encode16(<<index::256>>, case: :lower)

  defp account_hash, do: "0x" <> Base.encode16(<<9::256>>, case: :lower)
end

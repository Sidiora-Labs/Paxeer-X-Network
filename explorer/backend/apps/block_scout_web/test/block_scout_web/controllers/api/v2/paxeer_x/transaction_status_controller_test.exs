defmodule BlockScoutWeb.API.V2.PaxeerX.TransactionStatusControllerTest do
  use BlockScoutWeb.ConnCase

  alias Explorer.Chain.PaxeerX.SequencerReceipt
  alias Plug.Conn

  @vectors Path.expand(
             "../../../../../../../explorer/test/support/fixture/paxeer_x/receipt_vectors.json",
             __DIR__
           )

  setup do
    original = Application.get_env(:explorer, SequencerReceipt)
    Application.put_env(:explorer, SequencerReceipt, router_url: nil)
    Application.put_env(:tesla, :adapter, Tesla.Adapter.Mint)

    on_exit(fn ->
      Application.put_env(:tesla, :adapter, Explorer.Mock.TeslaAdapter)

      if original do
        Application.put_env(:explorer, SequencerReceipt, original)
      else
        Application.delete_env(:explorer, SequencerReceipt)
      end
    end)

    vectors = @vectors |> File.read!() |> Jason.decode!()

    {:ok, cases: Map.new(vectors["sequencer"]["cases"], &{&1["id"], &1})}
  end

  describe "GET /api/v2/transactions/:transaction_hash_param/status" do
    test "rejects a malformed hash", %{conn: conn} do
      request = get(conn, "/api/v2/transactions/0x01/status")

      assert %{"message" => "Invalid parameter(s)"} = json_response(request, 422)
    end

    test "answers 404 for a hash the chain does not carry", %{conn: conn} do
      serve_router(%{"pax_sequencerReceipt" => nil})
      hash = to_string(build(:transaction).hash)

      request = get(conn, "/api/v2/transactions/#{hash}/status")

      assert %{"message" => "Not found"} = json_response(request, 404)
      assert received_calls() == [{"pax_sequencerReceipt", [hash]}]
    end

    test "answers 404 for an unindexed hash while no router is configured", %{conn: conn} do
      hash = to_string(build(:transaction).hash)

      request = get(conn, "/api/v2/transactions/#{hash}/status")

      assert %{"message" => "Not found"} = json_response(request, 404)
    end

    test "a committed block answers committed with the three fields", %{conn: conn} do
      insert(:block, number: 99)
      block = insert(:block, number: 100)
      transaction = :transaction |> insert() |> with_block(block)

      response = json_response(get(conn, "/api/v2/transactions/#{transaction.hash}/status"), 200)

      assert response == %{"rung" => "committed", "block_number" => 100, "committed_height" => 100}
    end

    test "a block below the committed head answers committed", %{conn: conn} do
      block = insert(:block, number: 99)
      insert(:block, number: 100)
      transaction = :transaction |> insert() |> with_block(block)

      response = json_response(get(conn, "/api/v2/transactions/#{transaction.hash}/status"), 200)

      assert response == %{"rung" => "committed", "block_number" => 99, "committed_height" => 100}
    end

    test "an indexed transaction without a block answers submitted while no router is configured", %{conn: conn} do
      transaction = insert(:transaction)

      response = json_response(get(conn, "/api/v2/transactions/#{transaction.hash}/status"), 200)

      assert response == %{"rung" => "submitted", "block_number" => nil, "committed_height" => nil}
    end

    test "a hash with a verified sequencer receipt and no indexed transaction answers receipted",
         %{conn: conn, cases: cases} do
      test_case = cases["V1-pax-transfer"]
      insert(:block, number: 100)

      hash = tx_hash(test_case)

      serve_router(%{
        "pax_sequencerReceipt" => test_case["receiptJson"],
        "px_getReceiptKeys" => [test_case["pinnedKey"]]
      })

      response = json_response(get(conn, "/api/v2/transactions/#{hash}/status"), 200)

      assert response == %{"rung" => "receipted", "block_number" => nil, "committed_height" => 100}
      assert received_calls() == [{"pax_sequencerReceipt", [hash]}, {"px_getReceiptKeys", []}]
    end

    test "an indexed transaction without a block and a verified sequencer receipt answers receipted",
         %{conn: conn, cases: cases} do
      test_case = cases["V1-pax-transfer"]
      {:ok, hash} = Explorer.Chain.string_to_full_hash(tx_hash(test_case))
      insert(:transaction, hash: hash)

      serve_router(%{
        "pax_sequencerReceipt" => test_case["receiptJson"],
        "px_getReceiptKeys" => [test_case["pinnedKey"]]
      })

      response = json_response(get(conn, "/api/v2/transactions/#{hash}/status"), 200)

      assert response == %{"rung" => "receipted", "block_number" => nil, "committed_height" => nil}
    end

    test "a hash whose sequencer receipt fails verification answers 404", %{conn: conn, cases: cases} do
      test_case = cases["V3-tampered-log"]

      serve_router(%{
        "pax_sequencerReceipt" => test_case["receiptJson"],
        "px_getReceiptKeys" => [test_case["pinnedKey"]]
      })

      request = get(conn, "/api/v2/transactions/#{tx_hash(test_case)}/status")

      assert %{"message" => "Not found"} = json_response(request, 404)
    end

    test "a hash whose receipt key is not pinned answers 404", %{conn: conn, cases: cases} do
      test_case = cases["V1-pax-transfer"]
      other_key = Map.put(test_case["pinnedKey"], "keyId", "0x" <> String.duplicate("01", 32))

      serve_router(%{
        "pax_sequencerReceipt" => test_case["receiptJson"],
        "px_getReceiptKeys" => [other_key]
      })

      request = get(conn, "/api/v2/transactions/#{tx_hash(test_case)}/status")

      assert %{"message" => "Not found"} = json_response(request, 404)
    end

    test "a hash the sequencer reports only as submitted answers 404", %{conn: conn, cases: cases} do
      test_case = cases["V1-pax-transfer"]
      hash = tx_hash(test_case)

      serve_router(%{
        "pax_sequencerReceipt" => %{"rung" => "submitted", "txHash" => hash, "nonce" => "0x0"},
        "px_getReceiptKeys" => [test_case["pinnedKey"]]
      })

      request = get(conn, "/api/v2/transactions/#{hash}/status")

      assert %{"message" => "Not found"} = json_response(request, 404)
    end
  end

  defp tx_hash(%{"rawTransaction" => "0x" <> raw}) do
    "0x" <> (raw |> Base.decode16!(case: :mixed) |> ExKeccak.hash_256() |> Base.encode16(case: :lower))
  end

  defp serve_router(results) do
    bypass = Bypass.open()
    test_pid = self()

    Bypass.expect(bypass, "POST", "/", fn conn ->
      {:ok, body, conn} = Conn.read_body(conn)
      %{"jsonrpc" => "2.0", "id" => id, "method" => method, "params" => params} = Jason.decode!(body)
      send(test_pid, {:router_call, method, params})

      conn
      |> Conn.put_resp_content_type("application/json")
      |> Conn.resp(200, Jason.encode!(%{"jsonrpc" => "2.0", "id" => id, "result" => Map.fetch!(results, method)}))
    end)

    Application.put_env(:explorer, SequencerReceipt, router_url: "http://localhost:#{bypass.port}/")
  end

  defp received_calls(calls \\ []) do
    receive do
      {:router_call, method, params} -> received_calls([{method, params} | calls])
    after
      0 -> Enum.reverse(calls)
    end
  end
end

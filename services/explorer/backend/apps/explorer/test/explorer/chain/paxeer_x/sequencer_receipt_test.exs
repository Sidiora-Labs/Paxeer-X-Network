defmodule Explorer.Chain.PaxeerX.SequencerReceiptTest do
  use ExUnit.Case, async: true

  alias Explorer.Chain.PaxeerX.SequencerReceipt

  @vectors Path.expand("../../../support/fixture/paxeer_x/receipt_vectors.json", __DIR__)
  @commits Path.expand("../../../support/fixture/paxeer_x/comet_commit.json", __DIR__)
  @uint64_max 18_446_744_073_709_551_615

  setup_all do
    vectors = @vectors |> File.read!() |> Jason.decode!()
    commits = @commits |> File.read!() |> Jason.decode!()

    {:ok,
     kernel: Map.new(vectors["receipts"], &{&1["name"], &1}),
     cases: Map.new(vectors["sequencer"]["cases"], &{&1["id"], &1}),
     commits: commits}
  end

  defp bytes("0x" <> hex), do: Base.decode16!(hex, case: :mixed)
  defp bytes(hex), do: Base.decode16!(hex, case: :mixed)

  defp hex(bytes), do: "0x" <> Base.encode16(bytes, case: :lower)

  defp pinned(%{"pinnedKey" => key}) do
    {:ok, [pinned]} = SequencerReceipt.decode_receipt_keys([key])
    pinned
  end

  defp tx_hash(%{"rawTransaction" => raw}), do: raw |> bytes() |> ExKeccak.hash_256()

  defp kernel_answer(vector) do
    %{"rung" => "receipted", "receipt" => "0x" <> vector["receipt"], "txHash" => hex(activity_id(vector))}
  end

  defp kernel_key(key_id, public_key) do
    %{key_id: key_id, public_key: bytes(public_key), first_height: 0, last_height: @uint64_max}
  end

  defp decoded(vector) do
    {:ok, receipt} = vector["receipt"] |> bytes() |> SequencerReceipt.decode()
    receipt
  end

  defp context_hash(vector), do: decoded(vector).context_hash

  defp activity_id(vector), do: decoded(vector).activity_id

  defp verify_case(test_case, keys \\ nil, opts \\ []) do
    SequencerReceipt.verify(
      tx_hash(test_case),
      test_case["receiptJson"],
      keys || [pinned(test_case)],
      Keyword.put_new(opts, :prev_seq_hash, test_case["prevSeqHash"])
    )
  end

  defp committed_opts(commits, fixture, eth_receipt, chain_id, batch \\ %{from_seq: 1, to_seq: 4}) do
    %{"commit" => commit, "validators" => validators} = commits[fixture]
    [eth_receipt: eth_receipt, batch: batch, chain_id: chain_id, commit: commit, validators: validators]
  end

  describe "layerxproof receipt vectors" do
    test "each valid vector decodes, re-encodes byte-exact and carries its signed digest", %{kernel: kernel} do
      for name <- ["valid-0", "valid-1", "valid-2"] do
        vector = kernel[name]
        receipt_bytes = bytes(vector["receipt"])

        assert {:ok, receipt} = SequencerReceipt.decode(receipt_bytes)
        assert SequencerReceipt.encode(receipt) == receipt_bytes
        assert receipt.protocol_version == 3
        assert receipt.activity_id == bytes(vector["activity_id"])
        assert receipt.global_sequence == String.to_integer(vector["global_sequence"])
        assert receipt.result_status == String.to_integer(vector["result_code"])
        assert receipt.module_id == String.to_integer(vector["module_id"])
        assert receipt.operation == String.to_integer(vector["operation"])
        assert receipt.asset == bytes(vector["asset"])
        assert receipt.amount == String.to_integer(vector["amount"])
        assert receipt.resulting_state_root == bytes(vector["resulting_state_root"])
        assert receipt.timestamp == String.to_integer(vector["timestamp"])
        assert SequencerReceipt.digest(receipt) == bytes(vector["digest"])
        assert byte_size(receipt.signature) == 64
      end
    end

    test "each valid vector verifies R2 against its pinned key", %{kernel: kernel} do
      for name <- ["valid-0", "valid-1", "valid-2"] do
        vector = kernel[name]
        receipt_bytes = bytes(vector["receipt"])

        assert {:ok, receipt} = SequencerReceipt.verify_signature(receipt_bytes, bytes(vector["public_key"]))
        assert SequencerReceipt.encode(receipt) == receipt_bytes
      end
    end

    test "kernel-layout vectors stop at R3 because their batch_id is not a sequencer height", %{kernel: kernel} do
      for name <- ["valid-0", "valid-1", "valid-2"] do
        vector = kernel[name]
        key = kernel_key(context_hash(vector), vector["public_key"])

        assert SequencerReceipt.verify(activity_id(vector), kernel_answer(vector), [key]) == {:error, :r3}
      end
    end

    test "a bit-flipped signature fails R2", %{kernel: kernel} do
      vector = kernel["bit-flipped-signature"]
      key = kernel_key(context_hash(vector), vector["public_key"])

      assert SequencerReceipt.verify_signature(bytes(vector["receipt"]), bytes(vector["public_key"])) ==
               {:error, :r2}

      assert SequencerReceipt.verify(activity_id(vector), kernel_answer(vector), [key]) == {:error, :r2}
    end

    test "a bit-flipped body fails R2", %{kernel: kernel} do
      vector = kernel["bit-flipped-body"]
      key = kernel_key(context_hash(vector), vector["public_key"])

      assert SequencerReceipt.verify_signature(bytes(vector["receipt"]), bytes(vector["public_key"])) ==
               {:error, :r2}

      assert SequencerReceipt.verify(activity_id(vector), kernel_answer(vector), [key]) == {:error, :r2}
    end

    test "a wrong sequencer key fails R3 under its own key id and R2 under the receipt's key id", %{kernel: kernel} do
      vector = kernel["wrong-sequencer-key"]
      other_sequencer = kernel_key(:crypto.hash(:sha256, bytes(vector["public_key"])), vector["public_key"])
      same_key_id = kernel_key(context_hash(vector), vector["public_key"])

      assert SequencerReceipt.verify(activity_id(vector), kernel_answer(vector), [other_sequencer]) ==
               {:error, :r3}

      assert SequencerReceipt.verify(activity_id(vector), kernel_answer(vector), [same_key_id]) ==
               {:error, :r2}
    end
  end

  describe "node-signed sequencer receipts" do
    test "every V1 and V2 receipt decodes, re-encodes byte-exact and verifies receipted", %{cases: cases} do
      accepted = cases |> Map.values() |> Enum.filter(&(&1["vector"] in ["V1", "V2"]))

      assert length(accepted) == 5

      for test_case <- accepted do
        receipt_bytes = bytes(test_case["signedReceipt"])

        assert {:ok, receipt} = SequencerReceipt.decode(receipt_bytes)
        assert SequencerReceipt.encode(receipt) == receipt_bytes
        assert hex(SequencerReceipt.digest(receipt)) == test_case["receiptDigest"]
        assert receipt.activity_id == tx_hash(test_case)
        assert verify_case(test_case) == {:receipted, pinned(test_case)}
      end
    end

    test "every V3 receipt is refused at its named step", %{cases: cases} do
      refused = cases |> Map.values() |> Enum.filter(&(&1["vector"] == "V3"))

      assert Enum.map(refused, &{&1["id"], &1["expected"]["refusedAt"]}) |> Enum.sort() == [
               {"V3-activity-id-not-raw-hash", "R4"},
               {"V3-height-outside-key-range", "R3"},
               {"V3-key-not-pinned", "R3"},
               {"V3-non-canonical-bytes", "R2"},
               {"V3-seq-hash-mismatch", "R6"},
               {"V3-tampered-log", "R5"}
             ]

      for test_case <- refused do
        step = test_case["expected"]["refusedAt"] |> String.downcase() |> String.to_existing_atom()

        assert {test_case["id"], verify_case(test_case)} == {test_case["id"], {:error, step}}
      end
    end

    test "a key whose height range excludes the batch fails R3", %{cases: cases} do
      test_case = cases["V1-pax-transfer"]
      key = pinned(test_case)

      assert verify_case(test_case, [%{key | last_height: 999}]) == {:error, :r3}
      assert verify_case(test_case, [%{key | first_height: 1001, last_height: 1999}]) == {:error, :r3}
    end

    test "a wrong transaction hash fails R4", %{cases: cases} do
      test_case = cases["V1-pax-transfer"]
      other_hash = tx_hash(cases["V1-erc20-approve"])

      assert SequencerReceipt.verify(other_hash, test_case["receiptJson"], [pinned(test_case)]) == {:error, :r4}
    end

    test "an answer field that differs from the receipt bytes fails R5", %{cases: cases} do
      test_case = cases["V1-pax-transfer"]
      answer = %{test_case["receiptJson"] | "gasUsed" => "0x1"}

      assert SequencerReceipt.verify(tx_hash(test_case), answer, [pinned(test_case)]) == {:error, :r5}
    end

    test "an answer without a receipted rung fails R1", %{cases: cases} do
      test_case = cases["V1-pax-transfer"]

      assert SequencerReceipt.verify(tx_hash(test_case), %{"rung" => "submitted"}, [pinned(test_case)]) ==
               {:error, :r1}
    end
  end

  describe "committed rung" do
    setup %{cases: cases, commits: commits} do
      test_case = cases["V1-erc20-approve"]
      answer = test_case["receiptJson"]

      eth_receipt = %{
        "transactionHash" => answer["txHash"],
        "blockNumber" => answer["height"],
        "status" => answer["status"],
        "gasUsed" => answer["gasUsed"],
        "logs" => Enum.map(answer["logs"], &Map.take(&1, ["address", "topics", "data"]))
      }

      {:ok, test_case: test_case, eth_receipt: eth_receipt, chain_id: commits["chain_id"]}
    end

    test "a landing block signed by four of four validators verifies committed", context do
      %{test_case: test_case, eth_receipt: eth_receipt, chain_id: chain_id, commits: commits} = context

      assert verify_case(test_case, nil, committed_opts(commits, "four_of_four", eth_receipt, chain_id)) ==
               {:committed, pinned(test_case), 1_000}
    end

    test "R8 refuses the two-of-four commit", context do
      %{test_case: test_case, eth_receipt: eth_receipt, chain_id: chain_id, commits: commits} = context

      assert verify_case(test_case, nil, committed_opts(commits, "two_of_four", eth_receipt, chain_id)) ==
               {:error, :r8}
    end

    test "R7 refuses a landing receipt with another gasUsed or a batch that does not cover seq", context do
      %{test_case: test_case, eth_receipt: eth_receipt, chain_id: chain_id, commits: commits} = context

      changed = %{eth_receipt | "gasUsed" => "0x1"}

      assert verify_case(test_case, nil, committed_opts(commits, "four_of_four", changed, chain_id)) ==
               {:error, :r7}

      uncovered = committed_opts(commits, "four_of_four", eth_receipt, chain_id, %{from_seq: 3, to_seq: 4})

      assert verify_case(test_case, nil, uncovered) == {:error, :r7}
    end
  end

  describe "router reads" do
    test "an unset router URL disables the upgrade" do
      assert SequencerReceipt.receipt_keys(nil) == :disabled
      assert SequencerReceipt.commit(1_000, nil) == :disabled
      assert SequencerReceipt.validators(1_000, nil) == :disabled
    end

    test "receipt keys decode from the router answer and malformed entries are refused" do
      key_id = String.duplicate("6b", 32)
      public_key = String.duplicate("01", 32)

      assert SequencerReceipt.decode_receipt_keys([
               %{
                 "keyId" => "0x" <> key_id,
                 "publicKey" => "0x" <> public_key,
                 "firstHeight" => 1_000,
                 "lastHeight" => "0x7cf"
               }
             ]) ==
               {:ok,
                [
                  %{
                    key_id: Base.decode16!(key_id, case: :lower),
                    public_key: Base.decode16!(public_key),
                    first_height: 1_000,
                    last_height: 1_999
                  }
                ]}

      assert SequencerReceipt.decode_receipt_keys([
               %{"keyId" => "0x" <> key_id, "publicKey" => "0x01", "firstHeight" => 1, "lastHeight" => 2}
             ]) == {:error, :malformed_receipt_keys}

      assert SequencerReceipt.decode_receipt_keys([
               %{"keyId" => "0x" <> key_id, "publicKey" => "0x" <> public_key, "firstHeight" => 3, "lastHeight" => 2}
             ]) == {:error, :malformed_receipt_keys}
    end
  end
end

defmodule Explorer.Chain.PaxeerX.StatusTest do
  use Explorer.DataCase, async: false

  alias Explorer.Chain.Block
  alias Explorer.Chain.PaxeerX.Anchor, as: AnchorRow
  alias Explorer.Chain.PaxeerX.Finality
  alias Explorer.Chain.PaxeerX.Finality.Heights
  alias Explorer.Chain.PaxeerX.SequencerReceipt
  alias Explorer.Chain.PaxeerX.Status

  @vectors Path.expand("../../../support/fixture/paxeer_x/receipt_vectors.json", __DIR__)
  @uint64_max 18_446_744_073_709_551_615

  setup do
    original = Application.get_env(:explorer, Finality)

    on_exit(fn ->
      if original do
        Application.put_env(:explorer, Finality, original)
      else
        Application.delete_env(:explorer, Finality)
      end
    end)

    :ok
  end

  describe "of/2 submitted rung" do
    test "an item no block holds is submitted" do
      status = Status.of(nil, %Heights{committed_height: 1_000})

      assert status == %Status{
               rung: :submitted,
               reason: :no_block,
               block_number: nil,
               committed_height: 1_000
             }
    end

    test "a block number above the committed head is submitted" do
      status = Status.of(1_001, %Heights{committed_height: 1_000})

      assert status == %Status{
               rung: :submitted,
               reason: :above_head,
               block_number: 1_001,
               committed_height: 1_000
             }
    end

    test "a block number with no committed head stays submitted" do
      assert Status.of(7, %Heights{}) == %Status{
               rung: :submitted,
               reason: :head_unknown,
               block_number: 7,
               committed_height: nil
             }
    end

    test "an absent committed height is not substituted with zero" do
      assert Status.of(0, %Heights{}) == %Status{
               rung: :submitted,
               reason: :head_unknown,
               block_number: 0,
               committed_height: nil
             }
    end

    test "an item without a block stays submitted when the committed head is unknown" do
      assert Status.of(nil, %Heights{}) == %Status{rung: :submitted, reason: :no_block}
    end

    test "a negative block number is refused" do
      assert_raise FunctionClauseError, fn -> Status.of(-1, %Heights{committed_height: 100}) end
    end
  end

  describe "of/2 committed rung" do
    test "a block number the committed head covers is committed" do
      assert Status.of(100, %Heights{committed_height: 100}) == %Status{
               rung: :committed,
               reason: :in_committed_block,
               block_number: 100,
               committed_height: 100
             }
    end

    test "a block below the committed head is committed" do
      assert Status.of(99, %Heights{committed_height: 100}) == %Status{
               rung: :committed,
               reason: :in_committed_block,
               block_number: 99,
               committed_height: 100
             }
    end

    test "a reported committed genesis height covers block zero" do
      assert Status.of(0, %Heights{committed_height: 0}) == %Status{
               rung: :committed,
               reason: :in_committed_block,
               block_number: 0,
               committed_height: 0
             }
    end

    test "the committed rung has no anchor observation" do
      refute Map.has_key?(Status.of(100, %Heights{committed_height: 100}), :anchor)
    end

    test "a negative committed height is refused" do
      assert_raise FunctionClauseError, fn -> Status.of(0, %Heights{committed_height: -1}) end
    end

    test "a non-integer committed height is refused" do
      assert_raise FunctionClauseError, fn -> Status.of(100, %Heights{committed_height: "100"}) end
    end
  end

  describe "of/3 receipted rung" do
    setup do
      vectors = @vectors |> File.read!() |> Jason.decode!()

      {:ok,
       kernel: Map.new(vectors["receipts"], &{&1["name"], &1}),
       cases: Map.new(vectors["sequencer"]["cases"], &{&1["id"], &1})}
    end

    test "a hash with a verified sequencer receipt and no block is receipted", %{cases: cases} do
      test_case = cases["V1-pax-transfer"]
      key = pinned(test_case)
      verification = verify_case(test_case)

      assert verification == {:receipted, key}

      assert Status.of(nil, %Heights{committed_height: 100}, verification) == %Status{
               rung: :receipted,
               reason: :sequencer_receipt,
               block_number: nil,
               committed_height: 100
             }

      assert Status.of(nil, %Heights{}, verification) == %Status{
               rung: :receipted,
               reason: :sequencer_receipt,
               block_number: nil,
               committed_height: nil
             }
    end

    test "a verified sequencer receipt whose block is above the committed head is receipted", %{cases: cases} do
      verification = verify_case(cases["V1-pax-transfer"])

      assert Status.of(101, %Heights{committed_height: 100}, verification) == %Status{
               rung: :receipted,
               reason: :sequencer_receipt,
               block_number: 101,
               committed_height: 100
             }
    end

    test "valid-0 carries a signature its pinned key verifies", %{kernel: kernel} do
      vector = kernel["valid-0"]

      assert {:ok, receipt} =
               SequencerReceipt.verify_signature(bytes(vector["receipt"]), bytes(vector["public_key"]))

      assert receipt.activity_id == bytes(vector["activity_id"])
    end

    test "valid-0 stops at R3 under the full checks and stays submitted with no block", %{kernel: kernel} do
      vector = kernel["valid-0"]
      verification = verify_kernel(vector)

      assert verification == {:error, :r3}

      assert Status.of(nil, %Heights{committed_height: 100}, verification) == %Status{
               rung: :submitted,
               reason: :no_block,
               block_number: nil,
               committed_height: 100
             }
    end

    test "a bit-flipped receipt body with no block stays submitted", %{kernel: kernel} do
      vector = kernel["bit-flipped-body"]
      verification = verify_kernel(vector)

      assert verification == {:error, :r2}

      assert Status.of(nil, %Heights{committed_height: 100}, verification) == %Status{
               rung: :submitted,
               reason: :no_block,
               block_number: nil,
               committed_height: 100
             }
    end

    test "a receipt refused at any step cannot promote a rung", %{cases: cases} do
      for step <- [:r1, :r2, :r3, :r4, :r5, :r6, :r7, :r8] do
        assert Status.rung(nil, %Heights{committed_height: 100}, {:error, step}) == :submitted
        assert Status.rung(101, %Heights{committed_height: 100}, {:error, step}) == :submitted
      end

      for {id, step} <- [
            {"V3-key-not-pinned", :r3},
            {"V3-height-outside-key-range", :r3},
            {"V3-activity-id-not-raw-hash", :r4},
            {"V3-seq-hash-mismatch", :r6},
            {"V3-tampered-log", :r5},
            {"V3-non-canonical-bytes", :r2}
          ] do
        test_case = cases[id]
        verification = verify_case(test_case)

        assert verification == {:error, step}, id
        assert Status.rung(nil, %Heights{committed_height: 100}, verification) == :submitted
      end
    end

    test "block 100 under committed height 100 is committed whatever the receipt says", %{cases: cases} do
      verification = verify_case(cases["V1-pax-transfer"])

      for verification <- [verification, {:error, :r2}, nil] do
        assert Status.of(100, %Heights{committed_height: 100}, verification) == %Status{
                 rung: :committed,
                 reason: :in_committed_block,
                 block_number: 100,
                 committed_height: 100
               }
      end
    end

    test "a verification that is not a SequencerReceipt.verify result is refused" do
      for verification <- [:ok, {:error, :r9}, {:receipted, nil}, {:committed, %{}, 0}, true] do
        assert_raise FunctionClauseError, fn ->
          Status.of(nil, %Heights{committed_height: 100}, verification)
        end
      end
    end
  end

  describe "rung/2" do
    test "answers only the supported rung for the supplied block evidence" do
      assert Status.rung(nil, %Heights{committed_height: 100}) == :submitted
      assert Status.rung(101, %Heights{committed_height: 100}) == :submitted
      assert Status.rung(100, %Heights{committed_height: 100}) == :committed
      assert Status.rung(99, %Heights{committed_height: 100}) == :committed
      assert Status.rung(0, %Heights{}) == :submitted
    end
  end

  describe "retired anchor reads" do
    test "no anchor reader is exported" do
      assert Code.ensure_loaded?(Finality)
      refute function_exported?(Finality, :read_anchor, 0)
      refute function_exported?(Finality, :read_anchor, 1)
    end

    test "no latest anchor query is exported" do
      assert Code.ensure_loaded?(Finality)
      refute function_exported?(Finality, :latest_anchor, 0)
      refute function_exported?(Finality, :latest_sealing_query, 0)
    end

    test "no live precompile reader is exported" do
      assert Code.ensure_loaded?(Finality)
      refute function_exported?(Finality, :live_anchor, 0)
      refute function_exported?(Finality, :live_anchor, 1)
      refute function_exported?(Finality, :anchor_capability_live?, 0)
    end

    test "no confirmation fallback is exported" do
      assert Code.ensure_loaded?(Finality)
      refute function_exported?(Finality, :instant_height, 0)
      refute function_exported?(Finality, :final_confirmations, 0)
    end
  end

  describe "heights/1" do
    test "an empty block index leaves the committed height absent" do
      assert Finality.heights() == %Heights{committed_height: nil}
      assert Status.rung(0, Finality.heights()) == :submitted
    end

    test "collects the highest indexed consensus block" do
      insert(:block, number: 99)
      block = insert(:block, number: 100)
      transaction = :transaction |> insert() |> with_block(block)
      insert(:block, number: 101, consensus: false)

      heights = Finality.heights()

      assert heights == %Heights{committed_height: 100}
      assert Status.rung(transaction.block_number, heights) == :committed
      assert Status.rung(101, heights) == :submitted
    end

    test "a non-consensus block cannot supply a committed height" do
      insert(:block, number: 100, consensus: false)

      assert Finality.heights() == %Heights{committed_height: nil}
      assert Status.rung(100, Finality.heights()) == :submitted
    end

    test "a block losing consensus is no longer the committed head" do
      insert(:block, number: 99)
      block = insert(:block, number: 100)
      assert Finality.heights() == %Heights{committed_height: 100}

      block |> Ecto.Changeset.change(consensus: false) |> Repo.update!()

      assert Finality.heights() == %Heights{committed_height: 99}
      assert Status.rung(100, Finality.heights()) == :submitted
    end

    test "an already collected committed height is reused" do
      insert(:block, number: 100)

      assert Finality.heights(committed_height: 99) == %Heights{committed_height: 99}
      assert Finality.heights(committed_height: nil) == %Heights{committed_height: nil}
      assert Finality.heights(committed_height: 0) == %Heights{committed_height: 0}
    end

    test "invalid committed height overrides are refused" do
      for height <- [-1, "100", 100.0, false] do
        assert_raise ArgumentError, "invalid committed height", fn ->
          Finality.heights(committed_height: height)
        end
      end
    end

    test "legacy anchor configuration and observations cannot promote a rung" do
      Application.put_env(:explorer, Finality,
        capability_source: Explorer.Chain.PaxeerX.Capabilities,
        final_confirmations: 12
      )

      anchor = insert_anchor(18, :final, 4_250)

      assert Finality.heights() == %Heights{committed_height: anchor.block_number}
      assert Status.rung(nil, Finality.heights()) == :submitted
      refute Map.has_key?(Status.of(anchor.block_number, Finality.heights()), :anchor)
      assert Status.rung(anchor.block_number, Finality.heights()) == :committed
    end

    test "anchor checkpoints cannot replace a missing consensus head" do
      anchor = insert_anchor(19, :submitted, 4_300)
      block = Repo.get_by!(Block, hash: anchor.block_hash)
      block |> Ecto.Changeset.change(consensus: false) |> Repo.update!()

      assert Finality.heights() == %Heights{committed_height: nil}
      assert Status.rung(0, Finality.heights()) == :submitted
    end
  end

  defp bytes("0x" <> hex), do: Base.decode16!(hex, case: :mixed)
  defp bytes(hex), do: Base.decode16!(hex, case: :mixed)

  defp pinned(%{"pinnedKey" => key}) do
    {:ok, [pinned]} = SequencerReceipt.decode_receipt_keys([key])
    pinned
  end

  defp verify_case(test_case) do
    tx_hash = test_case["rawTransaction"] |> bytes() |> ExKeccak.hash_256()

    SequencerReceipt.verify(tx_hash, test_case["receiptJson"], [pinned(test_case)],
      prev_seq_hash: test_case["prevSeqHash"]
    )
  end

  defp verify_kernel(vector) do
    {:ok, receipt} = vector["receipt"] |> bytes() |> SequencerReceipt.decode()

    key = %{
      key_id: receipt.context_hash,
      public_key: bytes(vector["public_key"]),
      first_height: 0,
      last_height: @uint64_max
    }

    answer = %{"rung" => "receipted", "receipt" => "0x" <> vector["receipt"], "txHash" => hex(receipt.activity_id)}

    SequencerReceipt.verify(receipt.activity_id, answer, [key])
  end

  defp hex(bytes), do: "0x" <> Base.encode16(bytes, case: :lower)

  defp insert_anchor(batch_number, status, sealed_height) do
    block = insert(:block, number: 5_000 + batch_number)
    transaction = :transaction |> insert() |> with_block(block)

    attributes = %{
      transaction_hash: transaction.hash,
      log_index: 0,
      block_hash: block.hash,
      block_number: block.number,
      block_consensus: true,
      batch_number: batch_number,
      checkpoint_id: "0x" <> Base.encode16(<<batch_number::256>>, case: :lower),
      state_root: to_string(block_hash()),
      receipt_root: to_string(block_hash()),
      signers: 5,
      status: status,
      kernel_height: sealed_height,
      sealed_height: sealed_height
    }

    %AnchorRow{}
    |> AnchorRow.changeset(attributes)
    |> Repo.insert!()
  end
end

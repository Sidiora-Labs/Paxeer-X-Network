defmodule Explorer.Chain.Import.Runner.PaxeerX.ReceiptsTest do
  use Explorer.DataCase

  alias Explorer.Chain.Import
  alias Explorer.Chain.Import.Runner.PaxeerX.Receipts
  alias Explorer.Chain.PaxeerX.Receipt

  describe "insert_kernel/3" do
    test "inserts kernel rows once and treats an identical replay as a no-op" do
      rows = [kernel_row(3, 7), kernel_row(3, 8)]

      assert {:ok, %{insert_paxeer_x_kernel_receipts: inserted}} = import_rows(rows)
      assert inserted |> Enum.map(& &1.kernel_sequence) |> Enum.sort() == [7, 8]

      assert {:ok, %{insert_paxeer_x_kernel_receipts: []}} = import_rows(rows)

      stored = Repo.all(Receipt)

      assert stored |> Enum.map(& &1.kernel_sequence) |> Enum.sort() == [7, 8]
      assert Enum.all?(stored, &(&1.origin == :kernel and &1.kernel_verification == :sequencer_verified))
    end

    test "refuses a replay whose verified provenance differs from the stored row" do
      row = kernel_row(3, 7)

      assert {:ok, %{insert_paxeer_x_kernel_receipts: [_]}} = import_rows([row])

      assert {:error, :insert_paxeer_x_kernel_receipts, :kernel_receipt_conflict, _} =
               import_rows([%{row | kernel_result_code: 1}])

      assert [%Receipt{kernel_result_code: 0}] = Repo.all(Receipt)
    end
  end

  defp import_rows(rows) do
    {:ok, multi} = Import.all_single_multi([Receipts], %{paxeer_x_receipts: %{params: rows}})

    Repo.transaction(multi)
  end

  defp kernel_row(batch, sequence) do
    activity_id = digest("activity-#{batch}-#{sequence}")
    batch_id = digest("batch-#{batch}")
    raw_sha256 = digest("raw-#{batch}")

    %{
      origin: :kernel,
      receipt_id: activity_id,
      payload_hash: digest("receipt-#{batch}-#{sequence}"),
      status: :batch_included,
      kernel_batch_number: batch,
      kernel_batch_id: batch_id,
      kernel_sequence: sequence,
      kernel_activity_id: activity_id,
      kernel_result_code: 0,
      kernel_receipt_sha256: digest("receipt-#{batch}-#{sequence}"),
      kernel_canonical_sha256: digest("canonical-#{batch}-#{sequence}"),
      kernel_batch_raw_sha256: raw_sha256,
      kernel_state_root: digest("root-#{batch}"),
      kernel_proof: %{
        "batch_id" => String.trim_leading(batch_id, "0x"),
        "header_hex" => "0a0b",
        "signature_hex" => String.duplicate("cd", 64),
        "raw_sha256" => String.trim_leading(raw_sha256, "0x")
      },
      kernel_verification: :sequencer_verified
    }
  end

  defp digest(label), do: "0x" <> Base.encode16(:crypto.hash(:sha256, label), case: :lower)
end

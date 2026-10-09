defmodule Explorer.Chain.Import.Runner.PaxeerX.Receipts do
  @moduledoc """
  Bulk imports `t:Explorer.Chain.PaxeerX.Receipt.t/0`.

  Rows carry their provenance class in `origin`. EVM-derived rows are keyed by the
  transaction hash and the log index of the log they were decoded from; kernel-origin
  rows are keyed by the relay batch number and the sequence of the activity inside it.
  A repeated key is left untouched rather than rewritten, so re-reading a range of
  blocks or replaying a relay batch is idempotent.
  """

  require Ecto.Query

  alias Ecto.{Changeset, Multi, Repo}
  alias Explorer.Chain.Import
  alias Explorer.Chain.PaxeerX.Receipt
  alias Explorer.Prometheus.Instrumenter

  @behaviour Import.Runner

  # milliseconds
  @timeout 60_000

  @type imported :: [Receipt.t()]

  @impl Import.Runner
  def ecto_schema_module, do: Receipt

  @impl Import.Runner
  def option_key, do: :paxeer_x_receipts

  @impl Import.Runner
  @spec imported_table_row() :: %{:value_description => binary(), :value_type => binary()}
  def imported_table_row do
    %{
      value_type: "[#{ecto_schema_module()}.t()]",
      value_description: "List of `t:#{ecto_schema_module()}.t/0`s"
    }
  end

  @impl Import.Runner
  @spec run(Multi.t(), list(), map()) :: Multi.t()
  def run(multi, changes_list, %{timestamps: timestamps} = options) do
    insert_options =
      options
      |> Map.get(option_key(), %{})
      |> Map.take(~w(on_conflict timeout)a)
      |> Map.put_new(:timeout, @timeout)
      |> Map.put(:timestamps, timestamps)

    {kernel_changes_list, evm_changes_list} = Enum.split_with(changes_list, &kernel_origin?/1)

    multi
    |> Multi.run(:insert_paxeer_x_receipts, fn repo, _ ->
      Instrumenter.block_import_stage_runner(
        fn -> insert(repo, evm_changes_list, insert_options) end,
        :block_referencing,
        :paxeer_x_receipts,
        :paxeer_x_receipts
      )
    end)
    |> Multi.run(:insert_paxeer_x_kernel_receipts, fn repo, _ ->
      Instrumenter.block_import_stage_runner(
        fn -> insert_kernel(repo, kernel_changes_list, insert_options) end,
        :block_referencing,
        :paxeer_x_receipts,
        :paxeer_x_kernel_receipts
      )
    end)
  end

  @impl Import.Runner
  def timeout, do: @timeout

  @doc """
  Inserts EVM-derived receipt rows, deduplicated on `(transaction_hash, log_index)`.
  """
  @spec insert(Repo.t(), [map()], %{required(:timeout) => timeout(), required(:timestamps) => Import.timestamps()}) ::
          {:ok, [Receipt.t()]}
          | {:error, [Changeset.t()]}
  def insert(repo, changes_list, %{timeout: timeout, timestamps: timestamps} = _options) when is_list(changes_list) do
    # Enforce PaxeerX.Receipt ShareLocks order (see docs: sharelock.md)
    ordered_changes_list = Enum.sort_by(changes_list, &{&1.transaction_hash, &1.log_index})

    {:ok, inserted} =
      Import.insert_changes_list(
        repo,
        ordered_changes_list,
        for: Receipt,
        returning: true,
        timeout: timeout,
        timestamps: timestamps,
        conflict_target: [:transaction_hash, :log_index],
        on_conflict: :nothing
      )

    {:ok, inserted}
  end

  @doc """
  Inserts kernel-origin receipt rows, deduplicated on `(kernel_batch_number, kernel_sequence)`.

  Only rows the changeset accepted reach this function, so every row carries the
  verified relay provenance the schema requires; a replayed batch inserts nothing.
  """
  @spec insert_kernel(Repo.t(), [map()], %{
          required(:timeout) => timeout(),
          required(:timestamps) => Import.timestamps()
        }) ::
          {:ok, [Receipt.t()]}
          | {:error, [Changeset.t()]}
  def insert_kernel(repo, changes_list, %{timeout: timeout, timestamps: timestamps} = _options)
      when is_list(changes_list) do
    # Enforce PaxeerX.Receipt ShareLocks order (see docs: sharelock.md)
    ordered_changes_list = Enum.sort_by(changes_list, &{&1.kernel_batch_number, &1.kernel_sequence})

    {:ok, inserted} =
      Import.insert_changes_list(
        repo,
        ordered_changes_list,
        for: Receipt,
        returning: true,
        timeout: timeout,
        timestamps: timestamps,
        conflict_target: [:kernel_batch_number, :kernel_sequence],
        on_conflict: :nothing
      )

    fields =
      ~w(origin receipt_id account payload_hash status kernel_batch_number kernel_batch_id kernel_sequence kernel_activity_id kernel_result_code kernel_receipt_sha256 kernel_canonical_sha256 kernel_batch_raw_sha256 kernel_state_root kernel_proof kernel_verification)a

    consistent =
      Enum.all?(ordered_changes_list, fn row ->
        existing =
          repo.get_by!(Receipt, kernel_batch_number: row.kernel_batch_number, kernel_sequence: row.kernel_sequence)

        Enum.all?(fields, fn field -> Map.get(existing, field) == Map.get(row, field) end)
      end)

    if consistent, do: {:ok, inserted}, else: {:error, :kernel_receipt_conflict}
  end

  defp kernel_origin?(%{origin: :kernel}), do: true
  defp kernel_origin?(_changes), do: false
end

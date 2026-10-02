defmodule Explorer.Chain.PaxeerX.Receipt do
  @moduledoc """
  A kernel receipt, with the provenance class it was observed through.

  `origin` separates the two classes:

  - `:evm` - a receipt decoded from a log on the EVM side, unique on the emitting
    transaction hash and the log index. These rows inherit the consensus of the block
    that produced them: `block_consensus` is the denormalized copy of `blocks.consensus`,
    and `only_consensus_query/0` reads the authoritative flag through the block
    association, so a reorged block's rows drop out of every read the same way token
    transfers do.
  - `:kernel` - a receipt read from the public LayerX relay/archive batch history,
    unique on the kernel batch number and sequence. These rows carry no EVM
    transaction, log or block fields; they carry the batch, sequence, activity, digest
    and proof provenance the receipt was verified with, and exist only once that
    evidence verified against the pinned network and sequencer key
    (`kernel_verification` is `:sequencer_verified`; the database enforces it).

  `status` is the receipt verification lattice the kernel publishes: unverified,
  sequencer signed, batch included, state proven, checkpoint finalised, settlement
  anchored.

  Changes in the schema should be reflected in the bulk import module:
  - `Explorer.Chain.Import.Runner.PaxeerX.Receipts`
  """
  use Explorer.Schema

  alias Explorer.Chain.{Block, Hash, Transaction}
  alias Explorer.Repo

  @statuses ~w(unverified sequencer_signed batch_included state_proven checkpoint_finalised settlement_anchored)a
  @origins ~w(evm kernel)a
  @kernel_verifications ~w(sequencer_verified)a

  @evm_required_attrs ~w(receipt_id status block_hash block_number transaction_hash log_index)a
  @evm_optional_attrs ~w(account payload_hash block_consensus)a
  @evm_fields ~w(transaction_hash log_index block_hash block_number block_consensus)a

  @kernel_required_attrs ~w(receipt_id status kernel_batch_number kernel_batch_id kernel_sequence kernel_activity_id kernel_result_code kernel_receipt_sha256 kernel_canonical_sha256 kernel_batch_raw_sha256 kernel_state_root kernel_proof kernel_verification)a
  @kernel_optional_attrs ~w(account payload_hash)a
  @kernel_status :batch_included
  @kernel_proof_keys ~w(header_hex signature_hex batch_id raw_sha256)

  @public_fields ~w(id origin receipt_id account payload_hash status transaction_hash log_index block_hash block_number block_consensus kernel_batch_number kernel_batch_id kernel_sequence kernel_activity_id kernel_result_code kernel_receipt_sha256 kernel_canonical_sha256 kernel_batch_raw_sha256 kernel_state_root kernel_proof kernel_verification inserted_at updated_at)a

  @typedoc """
  * `origin` - the provenance class: `:evm` or `:kernel`.
  * `receipt_id` - the kernel identifier of the receipt (the activity id for kernel rows).
  * `account` - the kernel account the receipt belongs to.
  * `payload_hash` - the hash of the receipt payload.
  * `status` - the rung the receipt has reached on the verification lattice.
  * `transaction_hash` - the hash of the transaction that emitted the log (EVM rows only).
  * `log_index` - the index of the log within its block (EVM rows only).
  * `block_hash` - the hash of the block that holds the transaction (EVM rows only).
  * `block_number` - the number of that block (EVM rows only).
  * `block_consensus` - denormalized copy of the block's consensus flag (EVM rows only).
  * `kernel_batch_number` - the kernel batch number that carries the receipt.
  * `kernel_batch_id` - the kernel batch id that carries the receipt.
  * `kernel_sequence` - the kernel global sequence of the activity.
  * `kernel_activity_id` - the kernel activity id.
  * `kernel_result_code` - the activity result code.
  * `kernel_receipt_sha256` - SHA-256 of the receipt bytes served by the relay.
  * `kernel_canonical_sha256` - SHA-256 of the canonical activity bytes.
  * `kernel_batch_raw_sha256` - SHA-256 of the raw canonical batch bytes.
  * `kernel_state_root` - the resulting state root of the batch.
  * `kernel_proof` - the sequencer proof the row was verified with
    (`"header_hex"`, `"signature_hex"`, `"batch_id"`).
  * `kernel_verification` - the verification the kernel evidence passed.
  """
  @primary_key {:id, :id, autogenerate: true}
  typed_schema "lx_receipts" do
    field(:origin, Ecto.Enum, values: @origins, default: :evm, null: false)
    field(:log_index, :integer)
    field(:block_number, :integer) :: Block.block_number() | nil
    field(:block_consensus, :boolean)
    field(:receipt_id, Hash.Full, null: false)
    field(:account, Hash.Full)
    field(:payload_hash, Hash.Full)
    field(:status, Ecto.Enum, values: @statuses, null: false)

    field(:kernel_batch_number, :integer)
    field(:kernel_batch_id, Hash.Full)
    field(:kernel_sequence, :integer)
    field(:kernel_activity_id, Hash.Full)
    field(:kernel_result_code, :integer)
    field(:kernel_receipt_sha256, Hash.Full)
    field(:kernel_canonical_sha256, Hash.Full)
    field(:kernel_batch_raw_sha256, Hash.Full)
    field(:kernel_state_root, Hash.Full)
    field(:kernel_proof, :map)
    field(:kernel_verification, Ecto.Enum, values: @kernel_verifications)

    belongs_to(:transaction, Transaction,
      foreign_key: :transaction_hash,
      references: :hash,
      type: Hash.Full
    )

    belongs_to(:block, Block,
      foreign_key: :block_hash,
      references: :hash,
      type: Hash.Full
    )

    timestamps()
  end

  @doc """
  The rungs of the receipt verification lattice the schema admits.
  """
  @spec statuses() :: [atom()]
  def statuses, do: @statuses

  @doc """
  The provenance classes the schema admits.
  """
  @spec origins() :: [atom()]
  def origins, do: @origins

  @doc """
  Changeset dispatching on `origin` (from the attributes, else the struct).

  An `:evm` receipt keeps the log-derived required attributes; a missing
  `block_consensus` is set to `true`, the consensus a freshly imported block holds. A
  `:kernel` receipt requires its batch, sequence, activity, digest and proof
  provenance, refuses every EVM transaction, log and block field, requires `receipt_id`
  to be the activity id, holds the `:batch_included` rung and must carry the
  `:sequencer_verified` verification.
  """
  @spec changeset(Ecto.Schema.t(), map()) :: Ecto.Changeset.t()
  def changeset(%__MODULE__{} = receipt, attrs) do
    case attrs |> attr(:origin) |> cast_origin(receipt.origin) do
      {:ok, :kernel} -> kernel_changeset(receipt, attrs)
      {:ok, :evm} -> evm_changeset(receipt, attrs)
      :error -> receipt |> change() |> add_error(:origin, "is invalid", validation: :inclusion)
    end
  end

  defp evm_changeset(receipt, attrs) do
    receipt
    |> cast(attrs, @evm_required_attrs ++ @evm_optional_attrs)
    |> put_change(:origin, :evm)
    |> validate_required(@evm_required_attrs)
    |> default_block_consensus()
    |> foreign_key_constraint(:transaction_hash)
    |> foreign_key_constraint(:block_hash)
    |> constraints()
  end

  defp kernel_changeset(receipt, attrs) do
    receipt
    |> cast(attrs, @kernel_required_attrs ++ @kernel_optional_attrs)
    |> put_change(:origin, :kernel)
    |> validate_required(@kernel_required_attrs)
    |> validate_no_evm_fields(attrs)
    |> validate_inclusion(:status, [@kernel_status])
    |> validate_inclusion(:kernel_verification, @kernel_verifications)
    |> validate_number(:kernel_batch_number, greater_than_or_equal_to: 0)
    |> validate_number(:kernel_sequence, greater_than_or_equal_to: 0)
    |> validate_receipt_id_is_activity_id()
    |> validate_kernel_proof()
    |> constraints()
  end

  defp constraints(changeset) do
    changeset
    |> unique_constraint([:transaction_hash, :log_index], name: :lx_receipts_transaction_hash_log_index_index)
    |> unique_constraint([:kernel_batch_number, :kernel_sequence],
      name: :lx_receipts_kernel_batch_number_kernel_sequence_index
    )
    |> check_constraint(:origin, name: :lx_receipts_origin_check)
    |> check_constraint(:origin, name: :lx_receipts_origin_fields)
  end

  @doc """
  One page of publicly served receipts, newest receipt id first, one row per receipt id
  (the newest), starting below `paging_id` when given.

  Serves every kernel row and the EVM rows whose block holds consensus. Only columns of
  `lx_receipts` and `blocks.consensus` are read.
  """
  @spec public_page(Hash.Full.t() | nil, pos_integer()) :: [t()]
  def public_page(paging_id, limit) when is_integer(limit) and limit > 0 do
    public_query()
    |> page_below(paging_id)
    |> limit(^limit)
    |> Repo.replica().all()
  end

  @doc """
  The newest publicly served row of the receipt id, or `nil`.
  """
  @spec public_get(Hash.Full.t()) :: t() | nil
  def public_get(receipt_id) do
    public_query()
    |> where([receipt], receipt.receipt_id == ^receipt_id)
    |> limit(1)
    |> Repo.replica().one()
  end

  defp public_query do
    from(receipt in __MODULE__,
      left_join: block in assoc(receipt, :block),
      as: :block,
      where: receipt.origin == :kernel or (receipt.origin == :evm and block.consensus == true),
      distinct: [desc: receipt.receipt_id],
      order_by: [desc: receipt.block_number, desc: receipt.log_index, desc: receipt.id],
      select: struct(receipt, ^@public_fields)
    )
  end

  defp page_below(query, nil), do: query
  defp page_below(query, paging_id), do: where(query, [receipt], receipt.receipt_id < ^paging_id)

  @doc """
  Query over the EVM receipts that belong to a consensus block.

  The consensus flag is read through the block association so that the result is
  correct even before `block_consensus` has been denormalized onto the row.
  """
  @spec only_consensus_query() :: Ecto.Query.t()
  def only_consensus_query do
    from(receipt in __MODULE__,
      inner_join: block in assoc(receipt, :block),
      as: :block,
      where: block.consensus == true
    )
  end

  @doc """
  Query selecting the receipts of the given block hashes, for denormalizing the loss of
  consensus onto `block_consensus`.
  """
  @spec lose_consensus_query([Hash.Full.t()]) :: Ecto.Query.t()
  def lose_consensus_query(block_hashes) when is_list(block_hashes) do
    from(receipt in __MODULE__, where: receipt.block_hash in ^block_hashes)
  end

  defp attr(attrs, key), do: Map.get(attrs, key, Map.get(attrs, Atom.to_string(key)))

  defp cast_origin(nil, default), do: {:ok, default}
  defp cast_origin(origin, _default) when origin in @origins, do: {:ok, origin}
  defp cast_origin(origin, _default) when origin in ~w(evm kernel), do: {:ok, String.to_existing_atom(origin)}
  defp cast_origin(_origin, _default), do: :error

  defp default_block_consensus(changeset) do
    if is_nil(get_field(changeset, :block_consensus)),
      do: put_change(changeset, :block_consensus, true),
      else: changeset
  end

  defp validate_no_evm_fields(changeset, attrs) do
    Enum.reduce(@evm_fields, changeset, fn field, acc ->
      if is_nil(attr(attrs, field)) and is_nil(get_field(acc, field)),
        do: acc,
        else: add_error(acc, field, "must be nil for a kernel receipt")
    end)
  end

  defp validate_receipt_id_is_activity_id(changeset) do
    receipt_id = get_field(changeset, :receipt_id)
    activity_id = get_field(changeset, :kernel_activity_id)

    if is_nil(receipt_id) or is_nil(activity_id) or receipt_id == activity_id,
      do: changeset,
      else: add_error(changeset, :receipt_id, "must be the kernel activity id")
  end

  defp validate_kernel_proof(changeset) do
    validate_change(changeset, :kernel_proof, fn :kernel_proof, proof ->
      missing =
        Enum.reject(@kernel_proof_keys, fn key ->
          value = Map.get(proof, key)
          is_binary(value) and value != ""
        end)

      if missing == [],
        do: [],
        else: [kernel_proof: "is missing #{Enum.join(missing, ", ")}"]
    end)
  end
end

defmodule Explorer.Chain.PaxeerX.Receipt.KernelCursor do
  @moduledoc """
  The durable ingestion cursor of one public relay/archive source.

  `last_batch` is the last batch whose receipts were imported. A refused batch leaves
  `last_batch` where it was and records `refusal_code` and `refused_batch`; the next
  accepted batch clears them.
  """
  use Explorer.Schema

  @refusal_codes ~w(network_mismatch sequencer_key_mismatch sequencer_id_mismatch source_identity_mismatch batch_unauthorized raw_digest_mismatch header_not_in_raw signature_not_in_raw not_sequencer_verified sequence_out_of_range receipt_digest_mismatch canonical_digest_mismatch malformed native_verification_failed authenticated_projection_mismatch import_failed cursor_changed relay_unreachable response_too_large)

  @typedoc """
  * `source` - the relay/archive base URL.
  * `last_batch` - the last batch whose receipts were imported.
  * `next_cursor` - the relay history cursor to continue from.
  * `refusal_code` - why the last attempted batch was refused, if it was.
  * `refused_batch` - the batch that was refused.
  """
  @primary_key {:source, :string, autogenerate: false}
  typed_schema "lx_kernel_receipt_cursors" do
    field(:source_identity, :string, null: false)
    field(:last_batch, :integer, default: 0, null: false)
    field(:next_cursor, :string)
    field(:refusal_code, :string)
    field(:refused_batch, :integer)

    timestamps()
  end

  @doc """
  The refusal codes a cursor admits.
  """
  @spec refusal_codes() :: [String.t()]
  def refusal_codes, do: @refusal_codes

  @spec changeset(Ecto.Schema.t(), map()) :: Ecto.Changeset.t()
  def changeset(%__MODULE__{} = cursor, attrs) do
    cursor
    |> cast(attrs, ~w(source source_identity last_batch next_cursor refusal_code refused_batch)a)
    |> validate_required(~w(source source_identity last_batch)a)
    |> validate_format(:source_identity, ~r/\A[0-9a-f]{64}\z/)
    |> validate_length(:source, min: 1)
    |> validate_number(:last_batch, greater_than_or_equal_to: 0)
    |> validate_number(:refused_batch, greater_than_or_equal_to: 0)
    |> validate_change(:refusal_code, fn :refusal_code, code ->
      if code in @refusal_codes or code =~ ~r/\Arelay_status_[1-5][0-9]{2}\z/,
        do: [], else: [refusal_code: "is invalid"]
    end)
    |> validate_refusal_pair()
  end

  defp validate_refusal_pair(changeset) do
    case {get_field(changeset, :refusal_code), get_field(changeset, :refused_batch)} do
      {nil, nil} -> changeset
      {code, batch} when is_binary(code) and is_integer(batch) -> changeset
      {nil, _batch} -> add_error(changeset, :refusal_code, "is required with refused_batch")
      {code, nil} when is_binary(code) -> changeset
    end
  end
end

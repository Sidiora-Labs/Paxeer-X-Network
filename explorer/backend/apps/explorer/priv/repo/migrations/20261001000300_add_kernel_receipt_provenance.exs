defmodule Explorer.Repo.Migrations.AddKernelReceiptProvenance do
  use Ecto.Migration

  def up do
    execute("ALTER TABLE lx_receipts DROP CONSTRAINT lx_receipts_pkey")

    alter table(:lx_receipts) do
      add(:id, :bigserial, primary_key: true)

      modify(:transaction_hash, :bytea, null: true)
      modify(:log_index, :integer, null: true)
      modify(:block_hash, :bytea, null: true)
      modify(:block_number, :bigint, null: true)
      modify(:block_consensus, :boolean, null: true, default: nil)

      add(:origin, :string, null: false, default: "evm")
      add(:kernel_batch_number, :bigint, null: true)
      add(:kernel_batch_id, :bytea, null: true)
      add(:kernel_sequence, :bigint, null: true)
      add(:kernel_activity_id, :bytea, null: true)
      add(:kernel_result_code, :integer, null: true)
      add(:kernel_receipt_sha256, :bytea, null: true)
      add(:kernel_canonical_sha256, :bytea, null: true)
      add(:kernel_batch_raw_sha256, :bytea, null: true)
      add(:kernel_state_root, :bytea, null: true)
      add(:kernel_proof, :jsonb, null: true)
      add(:kernel_verification, :string, null: true)
    end

    create(constraint(:lx_receipts, :lx_receipts_origin_check, check: "origin IN ('evm', 'kernel')"))

    create(
      constraint(:lx_receipts, :lx_receipts_origin_fields,
        check: """
        (origin = 'evm' AND transaction_hash IS NOT NULL AND log_index IS NOT NULL AND block_hash IS NOT NULL
          AND block_number IS NOT NULL AND block_consensus IS NOT NULL)
        OR (origin = 'kernel' AND kernel_batch_number IS NOT NULL AND kernel_sequence IS NOT NULL
          AND kernel_activity_id IS NOT NULL AND kernel_receipt_sha256 IS NOT NULL
          AND kernel_batch_raw_sha256 IS NOT NULL AND kernel_proof IS NOT NULL
          AND kernel_batch_id IS NOT NULL AND kernel_canonical_sha256 IS NOT NULL
          AND kernel_state_root IS NOT NULL AND kernel_result_code IS NOT NULL
          AND receipt_id = kernel_activity_id AND status = 'batch_included'
          AND transaction_hash IS NULL AND log_index IS NULL AND block_hash IS NULL
          AND block_number IS NULL AND block_consensus IS NULL
          AND kernel_verification IS NOT NULL AND kernel_verification = 'sequencer_verified')
        """
      )
    )

    create(
      unique_index(:lx_receipts, [:transaction_hash, :log_index], name: :lx_receipts_transaction_hash_log_index_index)
    )

    create(
      unique_index(:lx_receipts, [:kernel_batch_number, :kernel_sequence],
        name: :lx_receipts_kernel_batch_number_kernel_sequence_index
      )
    )

    create(index(:lx_receipts, [:origin]))

    create table(:lx_kernel_receipt_cursors, primary_key: false) do
      add(:source, :string, null: false, primary_key: true)
      add(:source_identity, :string, null: false)
      add(:last_batch, :bigint, null: false, default: 0)
      add(:next_cursor, :string, null: true)
      add(:refusal_code, :string, null: true)
      add(:refused_batch, :bigint, null: true)

      timestamps(null: false, type: :utc_datetime_usec)
    end
  end

  def down do
    drop(table(:lx_kernel_receipt_cursors))

    execute("DELETE FROM lx_receipts WHERE origin <> 'evm'")

    drop(index(:lx_receipts, [:origin]))
    drop(index(:lx_receipts, [:kernel_batch_number, :kernel_sequence], name: :lx_receipts_kernel_batch_number_kernel_sequence_index))
    drop(index(:lx_receipts, [:transaction_hash, :log_index], name: :lx_receipts_transaction_hash_log_index_index))
    drop(constraint(:lx_receipts, :lx_receipts_origin_fields))
    drop(constraint(:lx_receipts, :lx_receipts_origin_check))

    alter table(:lx_receipts) do
      remove(:kernel_verification)
      remove(:kernel_proof)
      remove(:kernel_state_root)
      remove(:kernel_batch_raw_sha256)
      remove(:kernel_canonical_sha256)
      remove(:kernel_receipt_sha256)
      remove(:kernel_result_code)
      remove(:kernel_activity_id)
      remove(:kernel_sequence)
      remove(:kernel_batch_id)
      remove(:kernel_batch_number)
      remove(:origin)
      remove(:id)

      modify(:transaction_hash, :bytea, null: false)
      modify(:log_index, :integer, null: false)
      modify(:block_hash, :bytea, null: false)
      modify(:block_number, :bigint, null: false)
      modify(:block_consensus, :boolean, null: false, default: true)
    end

    execute("ALTER TABLE lx_receipts ADD CONSTRAINT lx_receipts_pkey PRIMARY KEY (transaction_hash, log_index)")
  end
end

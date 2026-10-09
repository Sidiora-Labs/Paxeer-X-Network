defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.ReceiptTest do
  use ExUnit.Case, async: true

  alias BlockScoutWeb.Schemas.API.V2.PaxeerX.Receipt

  test "lists every verification rung a receipt reports" do
    assert Receipt.schema().properties.verification.enum == [
             "sequencer_verified",
             "unverified",
             "sequencer_signed",
             "batch_included",
             "state_proven",
             "checkpoint_finalised",
             "settlement_anchored"
           ]
  end

  test "describes kernel and evm provenance as two closed alternatives" do
    schema = Receipt.schema()

    assert schema.required == [:id, :account, :status, :block_number, :origin, :verification, :provenance]

    assert [kernel, evm] = schema.properties.provenance.oneOf

    assert kernel.required == [:kernel]
    assert kernel.additionalProperties == false

    assert kernel.properties.kernel.required == [
             :batch_number,
             :batch_id,
             :sequence,
             :activity_id,
             :result_code,
             :receipt_sha256,
             :canonical_sha256,
             :batch_raw_sha256,
             :state_root,
             :proof
           ]

    proof = kernel.properties.kernel.properties.proof

    assert proof.required == [:header_hex, :signature_hex]
    assert proof.properties.signature_hex.pattern == "^[0-9a-f]{128}$"

    assert evm.required == [:evm]
    assert evm.additionalProperties == false
    assert evm.properties.evm.required == [:transaction_hash, :log_index, :block_hash, :block_number]
    assert evm.properties.evm.properties.log_index.minimum == 0
  end
end

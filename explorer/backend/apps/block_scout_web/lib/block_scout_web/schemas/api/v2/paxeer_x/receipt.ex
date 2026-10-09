defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.Receipt do
  @moduledoc """
  This module defines the schema for one kernel receipt in a list of them.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.General
  alias BlockScoutWeb.Schemas.API.V2.PaxeerX.SettlementRung
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "PaxeerXReceipt",
    description:
      "One kernel receipt as the newest log of its id reports it: the receipt id, the kernel account " <>
        "it belongs to and the settlement rung of the block it was last seen in.",
    type: :object,
    properties: %{
      id: General.FullHash,
      account: General.FullHashNullable,
      status: SettlementRung,
      block_number: %Schema{type: :integer, nullable: true, description: "EVM block, absent for kernel provenance"},
      origin: %Schema{type: :string, enum: ["kernel", "evm"]},
      verification: %Schema{
        type: :string,
        enum: [
          "sequencer_verified",
          "unverified",
          "sequencer_signed",
          "batch_included",
          "state_proven",
          "checkpoint_finalised",
          "settlement_anchored"
        ]
      },
      provenance: %Schema{
        oneOf: [
          %Schema{
            type: :object,
            additionalProperties: false,
            required: [:kernel],
            properties: %{
              kernel: %Schema{
                type: :object,
                additionalProperties: false,
                required: [
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
                ],
                properties: %{
                  batch_number: %Schema{type: :string, pattern: "^(0|[1-9][0-9]*)$"},
                  batch_id: General.FullHash,
                  sequence: %Schema{type: :string, pattern: "^(0|[1-9][0-9]*)$"},
                  activity_id: General.FullHash,
                  result_code: %Schema{type: :integer},
                  receipt_sha256: General.FullHash,
                  canonical_sha256: General.FullHash,
                  batch_raw_sha256: General.FullHash,
                  state_root: General.FullHash,
                  proof: %Schema{
                    type: :object,
                    additionalProperties: false,
                    required: [:header_hex, :signature_hex],
                    properties: %{
                      header_hex: %Schema{type: :string, pattern: "^[0-9a-f]+$"},
                      signature_hex: %Schema{type: :string, pattern: "^[0-9a-f]{128}$"}
                    }
                  }
                }
              }
            }
          },
          %Schema{
            type: :object,
            additionalProperties: false,
            required: [:evm],
            properties: %{
              evm: %Schema{
                type: :object,
                additionalProperties: false,
                required: [:transaction_hash, :log_index, :block_hash, :block_number],
                properties: %{
                  transaction_hash: General.FullHash,
                  log_index: %Schema{type: :integer, minimum: 0},
                  block_hash: General.FullHash,
                  block_number: %Schema{type: :integer, minimum: 0}
                }
              }
            }
          }
        ]
      }
    },
    required: [:id, :account, :status, :block_number, :origin, :verification, :provenance],
    nullable: false,
    additionalProperties: false
  })
end

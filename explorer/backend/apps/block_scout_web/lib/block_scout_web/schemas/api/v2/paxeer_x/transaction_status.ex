defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.TransactionStatus do
  @moduledoc """
  This module defines the schema for the rung a transaction has reached on the Paxeer X Network
  settlement ladder.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.PaxeerX.SettlementRung
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "PaxeerXTransactionStatus",
    description:
      "The rung a transaction has reached on the settlement ladder, with the block holding it and the " <>
        "committed height that rung was measured against. A height the explorer does not know is null " <>
        "rather than zero.",
    type: :object,
    properties: %{
      rung: SettlementRung,
      block_number: %Schema{
        type: :integer,
        minimum: 0,
        nullable: true,
        description: "Block holding the transaction, null while no indexed block holds it"
      },
      committed_height: %Schema{
        type: :integer,
        minimum: 0,
        nullable: true,
        description: "Highest indexed consensus block, null while the explorer has indexed none"
      }
    },
    required: [:rung, :block_number, :committed_height],
    nullable: false,
    additionalProperties: false
  })
end

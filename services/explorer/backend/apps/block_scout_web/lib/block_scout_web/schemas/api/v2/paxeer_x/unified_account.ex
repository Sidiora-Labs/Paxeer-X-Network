defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.UnifiedAccount do
  @moduledoc """
  This module defines the schema for the one-account view of the Paxeer X Network.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.PaxeerX.UnifiedAccount.{ActivityItem, Balance, Identities}
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "PaxeerXUnifiedAccount",
    description:
      "The one-account view of an EVM address: its four identities, one asset list where each asset " <>
        "carries a single total beside its chain, custody and kernel parts, and one activity feed " <>
        "merging the chain's transactions and token transfers with the LayerX kernel events.",
    type: :object,
    properties: %{
      identities: Identities,
      balances: %Schema{type: :array, items: Balance, nullable: false},
      activity: %Schema{type: :array, items: ActivityItem, nullable: false},
      next_page_params: %Schema{
        type: :object,
        nullable: true,
        properties: %{cursor: %Schema{type: :string}},
        required: [:cursor],
        additionalProperties: false
      },
      page_cursor: %Schema{type: :string},
      first_page_cursor: %Schema{type: :string},
      page_number: %Schema{type: :integer, minimum: 1},
      activity_total: %Schema{type: :integer, minimum: 0, nullable: true}
    },
    required: [
      :identities,
      :balances,
      :activity,
      :next_page_params,
      :page_cursor,
      :first_page_cursor,
      :page_number,
      :activity_total
    ],
    nullable: false,
    additionalProperties: false
  })
end

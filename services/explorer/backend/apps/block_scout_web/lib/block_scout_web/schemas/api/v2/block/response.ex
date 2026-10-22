defmodule BlockScoutWeb.Schemas.API.V2.Block.Response do
  @moduledoc """
  This module defines the schema for block response from /api/v2/blocks/:hash_or_number.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.Block
  alias BlockScoutWeb.Schemas.Helper
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(
    Block.schema()
    |> Helper.extend_schema(
      title: "BlockResponse",
      description: "Block response",
      properties: %{
        price_update_count: %Schema{
          type: :integer,
          minimum: 0,
          nullable: false,
          description: "Native price-feed submissions the block carries"
        }
      }
    )
  )
end

defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.PriceFeed.Observation do
  @moduledoc """
  This module defines the schema for one market observation of a native price-feed submission.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.General
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "PaxeerXPriceObservation",
    description: "One market observation of a price-feed submission; prices are 18-decimal integers.",
    type: :object,
    properties: %{
      market_id: %Schema{type: :integer, minimum: 0, nullable: false},
      symbol: %Schema{type: :string, nullable: true, description: "Market symbol, null while the market is unknown"},
      index_price: General.IntegerString,
      mark_price: General.IntegerString,
      source_timestamp_ms: %Schema{type: :integer, nullable: false},
      sequence: %Schema{type: :integer, nullable: false}
    },
    required: [:market_id, :symbol, :index_price, :mark_price, :source_timestamp_ms, :sequence],
    nullable: false,
    additionalProperties: false
  })
end

defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.PriceFeed.Update do
  @moduledoc """
  This module defines the schema for a native price-feed submission, shaped as a transaction list item.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.{General, PaxeerX.PriceFeed.Observation}
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "PaxeerXPriceUpdate",
    description:
      "A native CometBFT price-feed submission. It carries no EVM transaction, so it reads as a " <>
        "transaction of type 127 with no recipient, no value and no fee.",
    type: :object,
    properties: %{
      hash: General.FullHash,
      block_number: %Schema{type: :integer, minimum: 0, nullable: false},
      block_hash: General.FullHash,
      timestamp: General.Timestamp,
      index: %Schema{type: :integer, minimum: 0, nullable: false, description: "Position in the CometBFT block"},
      from: %Schema{
        type: :object,
        properties: %{
          hash: General.AddressHash,
          is_contract: %Schema{type: :boolean, nullable: false}
        },
        required: [:hash, :is_contract],
        nullable: false,
        additionalProperties: false
      },
      to: %Schema{type: :object, nullable: true, description: "Always null: a submission has no recipient"},
      value: %Schema{type: :string, nullable: false, enum: ["0"]},
      fee: %Schema{
        type: :object,
        properties: %{
          type: %Schema{type: :string, nullable: false, enum: ["actual"]},
          value: %Schema{type: :string, nullable: false, enum: ["0"]}
        },
        required: [:type, :value],
        nullable: false,
        additionalProperties: false
      },
      gas_used: General.IntegerString,
      gas_limit: General.IntegerString,
      status: %Schema{type: :string, nullable: false, enum: ["ok", "error"]},
      result: %Schema{type: :string, nullable: false, enum: ["success", "failed"]},
      method: %Schema{type: :string, nullable: false, enum: ["submitPrices"]},
      transaction_types: %Schema{
        type: :array,
        items: %Schema{type: :string, enum: ["price_update"]},
        nullable: false
      },
      type: %Schema{type: :integer, nullable: false, enum: [127]},
      confirmations: %Schema{type: :integer, minimum: 1, nullable: false},
      price_update: %Schema{
        type: :object,
        properties: %{
          feeder_bech32: %Schema{type: :string, nullable: false},
          observation_count: %Schema{type: :integer, minimum: 0, nullable: false},
          observations: %Schema{type: :array, items: Observation, nullable: false}
        },
        required: [:feeder_bech32, :observation_count, :observations],
        nullable: false,
        additionalProperties: false
      }
    },
    required: [
      :hash,
      :block_number,
      :block_hash,
      :timestamp,
      :index,
      :from,
      :to,
      :value,
      :fee,
      :gas_used,
      :gas_limit,
      :status,
      :result,
      :method,
      :transaction_types,
      :type,
      :confirmations,
      :price_update
    ],
    nullable: false,
    additionalProperties: false
  })
end

defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.PriceFeed.Latest do
  @moduledoc """
  This module defines the schema for the latest observation of a price-feed market.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.General
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "PaxeerXPriceMarketUpdate",
    description: "One observation of a market with the submission that carried it.",
    type: :object,
    properties: %{
      update_hash: General.FullHash,
      block_number: %Schema{type: :integer, minimum: 0, nullable: false},
      timestamp: General.Timestamp,
      index_price: General.IntegerString,
      mark_price: General.IntegerString,
      source_timestamp_ms: %Schema{type: :integer, nullable: false},
      sequence: %Schema{type: :integer, nullable: false}
    },
    required: [:update_hash, :block_number, :timestamp, :index_price, :mark_price, :source_timestamp_ms, :sequence],
    nullable: false,
    additionalProperties: false
  })
end

defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.PriceFeed.Markets do
  @moduledoc """
  This module defines the schema for the price-feed markets overview.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.PaxeerX.PriceFeed.Latest
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "PaxeerXPriceMarkets",
    description: "Every known price-feed market with its latest consensus observation, and the chain head.",
    type: :object,
    properties: %{
      items: %Schema{
        type: :array,
        nullable: false,
        items: %Schema{
          type: :object,
          properties: %{
            market_id: %Schema{type: :integer, minimum: 0, nullable: false},
            symbol: %Schema{type: :string, nullable: false},
            chainflow_symbol: %Schema{type: :string, nullable: false},
            latest: %{Latest.schema() | title: nil, nullable: true}
          },
          required: [:market_id, :symbol, :chainflow_symbol, :latest],
          nullable: false,
          additionalProperties: false
        }
      },
      chain_head: %Schema{type: :integer, minimum: 0, nullable: false}
    },
    required: [:items, :chain_head],
    nullable: false,
    additionalProperties: false
  })
end

defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.PriceFeed.MarketUpdates do
  @moduledoc """
  This module defines the schema for a page of one price-feed market's observations.
  """
  require OpenApiSpex

  alias BlockScoutWeb.Schemas.API.V2.PaxeerX.PriceFeed.Latest
  alias OpenApiSpex.Schema

  OpenApiSpex.schema(%{
    title: "PaxeerXPriceMarketUpdates",
    description: "A page of one market's consensus observations, newest first, with the market's symbol.",
    type: :object,
    properties: %{
      symbol: %Schema{type: :string, nullable: true, description: "Market symbol, null while the market is unknown"},
      items: %Schema{type: :array, items: Latest, nullable: false},
      next_page_params: %Schema{
        type: :object,
        nullable: true,
        example: %{"block_number" => 1_000, "index" => 0, "items_count" => 50}
      }
    },
    required: [:symbol, :items, :next_page_params],
    nullable: false,
    additionalProperties: false
  })
end

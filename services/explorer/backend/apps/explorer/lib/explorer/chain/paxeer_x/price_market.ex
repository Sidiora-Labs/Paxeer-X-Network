defmodule Explorer.Chain.PaxeerX.PriceMarket do
  @moduledoc """
  A price-feed market as the price-feed precompile lists it: its id, its symbol and the
  Chainflow symbol it is sourced from. `refreshed_at` is when the precompile last
  answered for it.
  """
  use Explorer.Schema

  @primary_key false
  typed_schema "paxeer_x_price_markets" do
    field(:market_id, :integer, primary_key: true, null: false)
    field(:symbol, :string, null: false)
    field(:chainflow_symbol, :string, null: false)
    field(:refreshed_at, :utc_datetime_usec, null: false)
  end
end

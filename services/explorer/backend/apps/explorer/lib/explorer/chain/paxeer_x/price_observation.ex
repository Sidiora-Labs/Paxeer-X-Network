defmodule Explorer.Chain.PaxeerX.PriceObservation do
  @moduledoc """
  One market observation of a native price-feed submission.

  Keyed by the submission hash and the market id. Prices are the 18-decimal integers
  the message carries. `block_number` is denormalized from the submission so a market's
  history reads from one index; the row goes when its submission goes.
  """
  use Explorer.Schema

  alias Explorer.Chain.{Block, Hash}
  alias Explorer.Chain.PaxeerX.PriceUpdate

  @typedoc """
  * `update_hash` - the hash of the submission that carries the observation.
  * `market_id` - the price-feed market the observation is for.
  * `block_number` - the height of the block that carries the submission.
  * `sequence` - the feeder's sequence for the market.
  * `index_price` - the index price, an 18-decimal integer.
  * `mark_price` - the mark price, an 18-decimal integer.
  * `source_timestamp_ms` - the source timestamp of the observation in milliseconds.
  """
  @primary_key false
  typed_schema "paxeer_x_price_observations" do
    field(:market_id, :integer, primary_key: true, null: false)
    field(:block_number, :integer, null: false) :: Block.block_number()
    field(:sequence, :integer, null: false)
    field(:index_price, :decimal, null: false)
    field(:mark_price, :decimal, null: false)
    field(:source_timestamp_ms, :integer, null: false)

    belongs_to(:update, PriceUpdate,
      foreign_key: :update_hash,
      primary_key: true,
      references: :hash,
      type: Hash.Full,
      null: false
    )
  end
end

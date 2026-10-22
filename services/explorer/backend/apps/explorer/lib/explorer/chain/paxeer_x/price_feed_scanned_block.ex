defmodule Explorer.Chain.PaxeerX.PriceFeedScannedBlock do
  @moduledoc """
  A block height whose CometBFT transactions have been scanned for price-feed
  submissions. Catch-up scans the heights that are in `blocks` and not here; a refetched
  block is scanned again and its row kept.
  """
  use Explorer.Schema

  @primary_key false
  typed_schema "paxeer_x_price_feed_scanned_blocks" do
    field(:block_number, :integer, primary_key: true, null: false)
  end
end

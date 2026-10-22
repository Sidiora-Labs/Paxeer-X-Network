defmodule Explorer.Chain.PaxeerX.PriceFeedScannedBlock do
  @moduledoc """
  A block height whose CometBFT transactions have been scanned for price-feed
  submissions. Catch-up scans the heights that are in `blocks` and not here; a refetched
  block is scanned again and its row kept.

  `status` is `"scanned"` once the submissions are stored, `"unavailable"` when every
  configured node answered that it no longer serves the height, and `"failed"` when the
  scan kept failing until its attempts ran out; `error` then holds the last reason. A
  later rescan selects the rows whose status is not `"scanned"`.
  """
  use Explorer.Schema

  @primary_key false
  typed_schema "paxeer_x_price_feed_scanned_blocks" do
    field(:block_number, :integer, primary_key: true, null: false)
    field(:status, :string, null: false)
    field(:error, :string)
  end
end

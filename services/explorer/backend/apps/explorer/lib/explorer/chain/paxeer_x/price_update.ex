defmodule Explorer.Chain.PaxeerX.PriceUpdate do
  @moduledoc """
  One native price-feed submission: a CometBFT transaction carrying a
  `MsgSubmitPrices` message, which never appears among the block's EVM transactions.

  Keyed by the transaction hash, the SHA-256 of the raw transaction bytes. `index` is the
  transaction's position in the CometBFT block, `status` is 1 when the transaction
  executed with code 0 and 0 otherwise, and `timestamp` is the block's timestamp. A
  deleted block takes its updates with it through the `block_hash` foreign key.
  """
  use Explorer.Schema

  alias Explorer.Chain.{Block, Hash}
  alias Explorer.Chain.PaxeerX.PriceObservation

  @typedoc """
  * `hash` - the CometBFT transaction hash.
  * `block_number` - the height of the block that carries the transaction.
  * `block_hash` - the hash of that block.
  * `index` - the position of the transaction in the CometBFT transaction list.
  * `feeder_hash` - the 20 bytes of the feeder account, spelled as an EVM address.
  * `feeder_bech32` - the feeder account as the message spells it.
  * `status` - 1 when the transaction executed with code 0, 0 when it failed.
  * `gas_used` - the gas the transaction used.
  * `gas_wanted` - the gas limit the transaction declared.
  * `observation_count` - how many market observations the submission carries.
  * `timestamp` - the timestamp of the block.
  """
  @primary_key false
  typed_schema "paxeer_x_price_updates" do
    field(:hash, Hash.Full, primary_key: true, null: false)
    field(:block_number, :integer, null: false) :: Block.block_number()
    field(:index, :integer, null: false)
    field(:feeder_hash, Hash.Address, null: false)
    field(:feeder_bech32, :string, null: false)
    field(:status, :integer, null: false)
    field(:gas_used, :decimal, null: false)
    field(:gas_wanted, :decimal, null: false)
    field(:observation_count, :integer, null: false)
    field(:timestamp, :utc_datetime_usec, null: false)

    belongs_to(:block, Block, foreign_key: :block_hash, references: :hash, type: Hash.Full, null: false)

    has_many(:observations, PriceObservation, foreign_key: :update_hash, references: :hash)

    timestamps()
  end
end

defmodule Explorer.Chain.PaxeerX.Finality.Anchor do
  @moduledoc """
  One anchor observation: the latest checkpoint the anchor reports, with the
  heights it covers and the source it was read from.

  * `:source` - `:anchor_table` for a row of `lx_anchors`, `:anchor_call` for a
    live read of the anchor precompile.
  * `:batch_number` - the batch of the latest checkpoint the anchor reports.
  * `:checkpoint_id` - the identifier of that checkpoint.
  * `:checkpoint_height` - the chain height the checkpoint itself records.
  * `:sealed_height` - the highest chain height the checkpoint seals.
  * `:finalized_batch_number` - the batch of the latest checkpoint the anchor
    reports as finalized, `nil` when the anchor reports none.
  * `:finalized_height` - the highest chain height covered by a checkpoint the
    anchor reports as finalized, `nil` when the anchor reports none. Never a
    substituted zero.
  * `:block_number` - the block the anchor row was observed in, known only on
    the database read.
  """

  @type t :: %__MODULE__{
          source: :anchor_table | :anchor_call,
          batch_number: non_neg_integer() | nil,
          checkpoint_id: String.t() | nil,
          checkpoint_height: non_neg_integer() | nil,
          sealed_height: non_neg_integer() | nil,
          finalized_batch_number: non_neg_integer() | nil,
          finalized_height: non_neg_integer() | nil,
          block_number: non_neg_integer() | nil
        }

  @enforce_keys [:source]
  defstruct [
    :source,
    :batch_number,
    :checkpoint_id,
    :checkpoint_height,
    :sealed_height,
    :finalized_batch_number,
    :finalized_height,
    :block_number
  ]
end

defmodule Explorer.Chain.PaxeerX.Finality.Heights do
  @moduledoc false

  @type t :: %__MODULE__{committed_height: non_neg_integer() | nil}

  defstruct committed_height: nil
end

defmodule Explorer.Chain.PaxeerX.Finality do
  @moduledoc false

  import Ecto.Query, only: [from: 2]

  alias Explorer.Chain
  alias Explorer.Chain.Block
  alias Explorer.Chain.PaxeerX.Finality.Heights

  @spec heights(keyword()) :: Heights.t()
  def heights(options \\ []) do
    committed_height = Keyword.get_lazy(options, :committed_height, fn -> committed_height(options) end)

    case committed_height do
      nil -> %Heights{}
      height when is_integer(height) and height >= 0 -> %Heights{committed_height: height}
      _invalid -> raise ArgumentError, "invalid committed height"
    end
  end

  defp committed_height(options) do
    query = from(block in Block, where: block.consensus == true, select: max(block.number))
    Chain.select_repo(options).one(query)
  end
end

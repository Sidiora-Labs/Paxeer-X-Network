defmodule Explorer.Chain.PaxeerX.Status do
  @moduledoc false

  alias Explorer.Chain.PaxeerX.Finality.Heights

  @type rung :: :submitted | :receipted | :committed
  @type reason :: :no_block | :above_head | :in_committed_block | :head_unknown

  @type t :: %__MODULE__{
          rung: rung(),
          reason: reason(),
          block_number: non_neg_integer() | nil,
          committed_height: non_neg_integer() | nil,
          anchor: nil
        }

  @enforce_keys [:rung, :reason]
  defstruct [:rung, :reason, :block_number, :committed_height, :anchor]

  @spec of(non_neg_integer() | nil, Heights.t()) :: t()
  def of(block_number, %Heights{} = heights)
      when is_nil(block_number) or (is_integer(block_number) and block_number >= 0) do
    {rung, reason} = decide(block_number, heights)

    %__MODULE__{
      rung: rung,
      reason: reason,
      block_number: block_number,
      committed_height: heights.committed_height
    }
  end

  @spec rung(non_neg_integer() | nil, Heights.t()) :: rung()
  def rung(block_number, %Heights{} = heights), do: of(block_number, heights).rung

  defp decide(nil, _heights), do: {:submitted, :no_block}
  defp decide(_block_number, %Heights{committed_height: nil}), do: {:submitted, :head_unknown}

  defp decide(block_number, %Heights{committed_height: committed_height})
       when is_integer(committed_height) and committed_height >= 0 do
    if block_number <= committed_height do
      {:committed, :in_committed_block}
    else
      {:submitted, :above_head}
    end
  end
end

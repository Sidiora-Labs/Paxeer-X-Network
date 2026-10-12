defmodule Explorer.Chain.PaxeerX.Status do
  @moduledoc false

  alias Explorer.Chain.PaxeerX.Finality.Heights
  alias Explorer.Chain.PaxeerX.SequencerReceipt

  @type rung :: :submitted | :receipted | :committed
  @type reason :: :no_block | :above_head | :in_committed_block | :head_unknown | :sequencer_receipt

  @type verification ::
          {:receipted, SequencerReceipt.receipt_key()}
          | {:committed, SequencerReceipt.receipt_key(), pos_integer()}
          | {:error, SequencerReceipt.step()}
          | nil

  @type t :: %__MODULE__{
          rung: rung(),
          reason: reason(),
          block_number: non_neg_integer() | nil,
          committed_height: non_neg_integer() | nil
        }

  @enforce_keys [:rung, :reason]
  defstruct [:rung, :reason, :block_number, :committed_height]

  @doc """
  Places an item on the settlement ladder from the block holding it, the committed
  head and the result of `SequencerReceipt.verify/4` on its sequencer receipt.

  A block the committed head covers is committed. Otherwise a sequencer receipt
  that passed R1-R6 is receipted; anything else stays submitted.
  """
  @spec of(non_neg_integer() | nil, Heights.t(), verification()) :: t()
  def of(block_number, %Heights{} = heights, verification \\ nil)
      when is_nil(block_number) or (is_integer(block_number) and block_number >= 0) do
    {rung, reason} =
      case {decide(block_number, heights), verified?(verification)} do
        {{:committed, _reason} = committed, _verified} -> committed
        {_submitted, true} -> {:receipted, :sequencer_receipt}
        {submitted, false} -> submitted
      end

    %__MODULE__{
      rung: rung,
      reason: reason,
      block_number: block_number,
      committed_height: heights.committed_height
    }
  end

  @spec rung(non_neg_integer() | nil, Heights.t(), verification()) :: rung()
  def rung(block_number, %Heights{} = heights, verification \\ nil), do: of(block_number, heights, verification).rung

  defp verified?({:receipted, %{key_id: <<_::256>>, public_key: <<_::256>>}}), do: true

  defp verified?({:committed, %{key_id: <<_::256>>, public_key: <<_::256>>}, height})
       when is_integer(height) and height > 0,
       do: true

  defp verified?(nil), do: false
  defp verified?({:error, step}) when step in [:r1, :r2, :r3, :r4, :r5, :r6, :r7, :r8], do: false

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

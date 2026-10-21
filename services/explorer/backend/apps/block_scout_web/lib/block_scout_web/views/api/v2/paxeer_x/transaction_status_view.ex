defmodule BlockScoutWeb.API.V2.PaxeerX.TransactionStatusView do
  @moduledoc """
  Renders the rung a transaction has reached on the Paxeer X Network settlement ladder, with
  the block holding it and the committed height that rung was measured against.
  """

  use BlockScoutWeb, :view

  alias Explorer.Chain.PaxeerX.Status

  def render("status.json", %{status: %Status{} = status}) do
    %{
      "rung" => to_string(status.rung),
      "block_number" => status.block_number,
      "committed_height" => status.committed_height
    }
  end
end

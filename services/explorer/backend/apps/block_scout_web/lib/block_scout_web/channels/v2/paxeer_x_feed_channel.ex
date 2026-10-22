defmodule BlockScoutWeb.V2.PaxeerXFeedChannel do
  @moduledoc """
  Establishes pub/sub channel for live updates of native price-feed submissions for API V2:
  `paxeer_x_feed:new_update` carries every new submission and `paxeer_x_feed:market:<id>`
  the observations of one market.
  """
  use BlockScoutWeb, :channel

  def join("paxeer_x_feed:new_update", _params, socket) do
    {:ok, %{}, socket}
  end

  def join("paxeer_x_feed:market:" <> market_id, _params, socket) do
    case Integer.parse(market_id) do
      {id, ""} when id >= 0 -> {:ok, %{}, socket}
      _ -> {:error, %{reason: "invalid market id"}}
    end
  end

  def join(_topic, _params, _socket) do
    {:error, %{reason: "unknown topic"}}
  end
end

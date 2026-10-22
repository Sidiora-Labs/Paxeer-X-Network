defmodule BlockScoutWeb.API.V2.PaxeerX.FeedView do
  @moduledoc """
  Renders native price-feed submissions and markets.

  A submission renders as a transaction list item of type 127 with no recipient, no value
  and no fee, so the transaction lists can carry it beside EVM transactions; its
  observations are listed in market order under `price_update`, each with the market's
  symbol when the market is known.
  """

  use BlockScoutWeb, :view

  alias Explorer.Chain.Address
  alias Explorer.Chain.PaxeerX.{PriceMarket, PriceObservation, PriceUpdate}

  @price_update_type 127

  def render("markets.json", %{markets: markets, chain_head: chain_head}) do
    %{"items" => Enum.map(markets, &prepare_market/1), "chain_head" => chain_head}
  end

  def render("updates.json", %{
        updates: updates,
        symbols: symbols,
        block_height: block_height,
        next_page_params: next_page_params
      }) do
    %{
      "items" => Enum.map(updates, &prepare_update(&1, symbols, block_height)),
      "next_page_params" => next_page_params
    }
  end

  def render("update.json", %{update: update, symbols: symbols, block_height: block_height}) do
    prepare_update(update, symbols, block_height)
  end

  def render("market_updates.json", %{symbol: symbol, observations: observations, next_page_params: next_page_params}) do
    %{
      "symbol" => symbol,
      "items" => Enum.map(observations, &prepare_observation_update/1),
      "next_page_params" => next_page_params
    }
  end

  @doc """
  Renders one submission as a transaction list item; `symbols` maps market ids to symbols and
  `block_height` is the highest consensus block number.
  """
  @spec prepare_update(PriceUpdate.t(), %{non_neg_integer() => String.t()}, non_neg_integer()) :: map()
  def prepare_update(%PriceUpdate{} = update, symbols, block_height) do
    succeeded? = update.status == 1

    %{
      "hash" => to_string(update.hash),
      "block_number" => update.block_number,
      "block_hash" => to_string(update.block_hash),
      "timestamp" => update.timestamp,
      "index" => update.index,
      "from" => %{"hash" => Address.checksum(update.feeder_hash), "is_contract" => false},
      "to" => nil,
      "value" => "0",
      "fee" => %{"type" => "actual", "value" => "0"},
      "gas_used" => integer_string(update.gas_used),
      "gas_limit" => integer_string(update.gas_wanted),
      "status" => if(succeeded?, do: "ok", else: "error"),
      "result" => if(succeeded?, do: "success", else: "failed"),
      "method" => "submitPrices",
      "transaction_types" => ["price_update"],
      "type" => @price_update_type,
      "confirmations" => max(1 + block_height - update.block_number, 1),
      "price_update" => %{
        "feeder_bech32" => update.feeder_bech32,
        "observation_count" => update.observation_count,
        "observations" =>
          update.observations
          |> Enum.sort_by(& &1.market_id)
          |> Enum.map(&prepare_observation(&1, symbols))
      }
    }
  end

  defp prepare_observation(%PriceObservation{} = observation, symbols) do
    %{
      "market_id" => observation.market_id,
      "symbol" => Map.get(symbols, observation.market_id),
      "index_price" => integer_string(observation.index_price),
      "mark_price" => integer_string(observation.mark_price),
      "source_timestamp_ms" => observation.source_timestamp_ms,
      "sequence" => observation.sequence
    }
  end

  defp prepare_market({%PriceMarket{} = market, latest}) do
    %{
      "market_id" => market.market_id,
      "symbol" => market.symbol,
      "chainflow_symbol" => market.chainflow_symbol,
      "latest" => latest && prepare_observation_update(latest)
    }
  end

  defp prepare_observation_update(%PriceObservation{update: %PriceUpdate{} = update} = observation) do
    %{
      "update_hash" => to_string(observation.update_hash),
      "block_number" => observation.block_number,
      "timestamp" => update.timestamp,
      "index_price" => integer_string(observation.index_price),
      "mark_price" => integer_string(observation.mark_price),
      "source_timestamp_ms" => observation.source_timestamp_ms,
      "sequence" => observation.sequence
    }
  end

  defp integer_string(%Decimal{} = value), do: Decimal.to_string(value, :normal)
  defp integer_string(value) when is_integer(value), do: Integer.to_string(value)
end

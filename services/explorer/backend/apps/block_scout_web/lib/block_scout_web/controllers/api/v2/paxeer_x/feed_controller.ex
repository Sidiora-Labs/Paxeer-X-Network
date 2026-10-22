defmodule BlockScoutWeb.API.V2.PaxeerX.FeedController do
  @moduledoc """
  Publishes the native price-feed submissions of Paxeer X Network.

  A submission is a CometBFT `MsgSubmitPrices` transaction, which no EVM block carries. The
  indexer reads them from the Cosmos REST API of the connected nodes; these endpoints read
  only the submissions of consensus blocks.
  """

  use BlockScoutWeb, :controller
  use OpenApiSpex.ControllerSpecs

  import BlockScoutWeb.Chain, only: [next_page_params: 5, split_list_by_page: 1]
  import Explorer.PagingOptions, only: [default_paging_options: 0]

  alias BlockScoutWeb.API.V2.PaxeerX.FeedView
  alias BlockScoutWeb.Schemas.API.V2.ErrorResponses.NotFoundResponse
  alias BlockScoutWeb.Schemas.API.V2.General
  alias Explorer.Chain
  alias Explorer.Chain.PaxeerX.{PriceFeed, PriceObservation, PriceUpdate}
  alias OpenApiSpex.Parameter

  action_fallback(BlockScoutWeb.API.V2.FallbackController)

  plug(OpenApiSpex.Plug.CastAndValidate, json_render_error_v2: true)

  @api_true [api?: true]

  @feeder_param %Parameter{
    name: :feeder,
    in: :query,
    schema: General.AddressHash,
    required: false,
    description: "Only submissions of this feeder address"
  }

  @market_id_query_param %Parameter{
    name: :market_id,
    in: :query,
    schema: %Schema{type: :integer, minimum: 0},
    required: false,
    description: "Only submissions that carry an observation of this market"
  }

  @market_id_path_param %Parameter{
    name: :market_id,
    in: :path,
    schema: %Schema{type: :integer, minimum: 0},
    required: true,
    description: "Price-feed market id in the path"
  }

  @hash_path_param %Parameter{
    name: :hash,
    in: :path,
    schema: General.FullHash,
    required: true,
    description: "Submission hash in the path"
  }

  @paging_example %{"block_number" => 1_000, "index" => 0, "items_count" => 50}

  tags(["paxeer-x"])

  operation :markets,
    summary: "List the price-feed markets of Paxeer X Network",
    description:
      "Retrieves every market the price-feed precompile lists, in market id order, each with its latest " <>
        "observation from a consensus block, and the highest consensus block number.",
    parameters: base_params(),
    responses: [
      ok: {"Price-feed markets.", "application/json", Schemas.PaxeerX.PriceFeed.Markets}
    ]

  @doc """
  Handles GET requests to `/api/v2/paxeer-x/feed/markets`.
  """
  @spec markets(Plug.Conn.t(), map()) :: Plug.Conn.t()
  def markets(conn, _params) do
    conn
    |> put_status(200)
    |> put_view(FeedView)
    |> render(:markets, %{markets: PriceFeed.latest_per_market(), chain_head: Chain.block_height(@api_true)})
  end

  operation :updates,
    summary: "List the price-feed submissions of Paxeer X Network",
    description:
      "Retrieves a paginated list of price-feed submissions, newest first by block number and CometBFT " <>
        "index, optionally only those of one feeder or only those that carry one market.",
    parameters:
      base_params() ++
        [@feeder_param, @market_id_query_param] ++
        define_paging_params(["block_number", "index", "items_count"]),
    responses: [
      ok:
        {"Price-feed submissions with pagination.", "application/json",
         paginated_response(items: Schemas.PaxeerX.PriceFeed.Update, next_page_params_example: @paging_example)},
      unprocessable_entity: JsonErrorResponse.response()
    ]

  @doc """
  Handles GET requests to `/api/v2/paxeer-x/feed/updates`.
  """
  @spec updates(Plug.Conn.t(), map()) :: Plug.Conn.t()
  def updates(conn, params) do
    {updates, next_page} =
      params
      |> paging_options()
      |> PriceFeed.list_updates(feeder: feeder(params[:feeder]), market_id: params[:market_id])
      |> split_list_by_page()

    conn
    |> put_status(200)
    |> put_view(FeedView)
    |> render(:updates, %{
      updates: updates,
      symbols: PriceFeed.update_market_symbols(updates),
      block_height: Chain.block_height(@api_true),
      next_page_params: next_page_params(next_page, updates, params, false, &update_paging_params/1)
    })
  end

  operation :update,
    summary: "Retrieve one price-feed submission of Paxeer X Network",
    description: "Retrieves one price-feed submission of a consensus block by its hash, with its observations.",
    parameters: [@hash_path_param | base_params()],
    responses: [
      ok: {"The price-feed submission.", "application/json", Schemas.PaxeerX.PriceFeed.Update},
      not_found: NotFoundResponse.response(),
      unprocessable_entity: JsonErrorResponse.response()
    ]

  @doc """
  Handles GET requests to `/api/v2/paxeer-x/feed/updates/:hash`.
  """
  @spec update(Plug.Conn.t(), map()) :: Plug.Conn.t() | {:error, :not_found}
  def update(conn, %{hash: hash}) do
    case PriceFeed.update_by_hash(hash) do
      %PriceUpdate{} = update ->
        conn
        |> put_status(200)
        |> put_view(FeedView)
        |> render(:update, %{
          update: update,
          symbols: PriceFeed.update_market_symbols([update]),
          block_height: Chain.block_height(@api_true)
        })

      nil ->
        {:error, :not_found}
    end
  end

  operation :market_updates,
    summary: "List the observations of one price-feed market of Paxeer X Network",
    description:
      "Retrieves a paginated list of one market's observations from consensus blocks, newest first, each " <>
        "with the submission that carried it, and the market's symbol, null while the market is unknown.",
    parameters:
      [@market_id_path_param | base_params()] ++ define_paging_params(["block_number", "index", "items_count"]),
    responses: [
      ok:
        {"Market observations with pagination, and the market's symbol.", "application/json",
         Schemas.PaxeerX.PriceFeed.MarketUpdates},
      unprocessable_entity: JsonErrorResponse.response()
    ]

  @doc """
  Handles GET requests to `/api/v2/paxeer-x/feed/markets/:market_id/updates`.
  """
  @spec market_updates(Plug.Conn.t(), map()) :: Plug.Conn.t()
  def market_updates(conn, %{market_id: market_id} = params) do
    {observations, next_page} =
      market_id
      |> PriceFeed.market_updates(paging_options(params))
      |> split_list_by_page()

    conn
    |> put_status(200)
    |> put_view(FeedView)
    |> render(:market_updates, %{
      symbol: Map.get(PriceFeed.market_symbols([market_id]), market_id),
      observations: observations,
      next_page_params:
        next_page_params(next_page, observations, Map.delete(params, :market_id), false, &observation_paging_params/1)
    })
  end

  defp paging_options(%{block_number: block_number, index: index}) when is_integer(block_number) and is_integer(index),
    do: %{default_paging_options() | key: {block_number, index}}

  defp paging_options(_params), do: default_paging_options()

  defp feeder(nil), do: nil

  defp feeder(feeder) do
    {:ok, feeder_hash} = Chain.string_to_address_hash(feeder)
    feeder_hash
  end

  defp update_paging_params(%PriceUpdate{block_number: block_number, index: index}),
    do: %{block_number: block_number, index: index}

  defp observation_paging_params(%PriceObservation{block_number: block_number, update: %PriceUpdate{index: index}}),
    do: %{block_number: block_number, index: index}
end

defmodule BlockScoutWeb.API.V2.PaxeerX.TransactionStatusController do
  @moduledoc """
  Publishes the rung a transaction has reached on the Paxeer X Network settlement ladder.

  An indexed transaction is placed from the block holding it and the committed head. A hash
  the explorer has not indexed is answered from the sequencer: its `pax_sequencerReceipt`
  answer is verified (R1-R6) against the pinned receipt keys, and only a verified receipt
  answers `receipted`; every other hash is not found.
  """

  use BlockScoutWeb, :controller
  use OpenApiSpex.ControllerSpecs

  alias BlockScoutWeb.Schemas.API.V2.ErrorResponses.NotFoundResponse
  alias Explorer.Chain
  alias Explorer.Chain.Hash
  alias Explorer.Chain.PaxeerX.{Finality, SequencerReceipt, Status}
  alias Explorer.HttpClient

  action_fallback(BlockScoutWeb.API.V2.FallbackController)

  @api_true [api?: true]
  @router_timeout 10_000

  tags(["paxeer-x"])

  operation :status,
    summary: "Retrieve the settlement status of a transaction on Paxeer X Network",
    description:
      "Retrieves the rung the transaction has reached on the settlement ladder: `committed` once a block " <>
        "the committed head covers holds it, `receipted` once its sequencer receipt verifies against the " <>
        "pinned receipt keys, `submitted` otherwise.",
    parameters: [transaction_hash_param() | base_params()],
    responses: [
      ok: {"The settlement status of the transaction.", "application/json", Schemas.PaxeerX.TransactionStatus},
      not_found: NotFoundResponse.response(),
      unprocessable_entity: {"Invalid parameter(s).", "application/json", message_response_schema()}
    ]

  @doc """
  Handles GET requests to `/api/v2/transactions/:transaction_hash_param/status`.

  Answers with the rung the transaction has reached, the block holding it and the committed
  height that rung was measured against.
  """
  @spec status(Plug.Conn.t(), map()) :: Plug.Conn.t() | {atom(), any()}
  def status(conn, %{"transaction_hash_param" => transaction_hash_string} = _params) do
    with {:format, {:ok, transaction_hash}} <- {:format, Chain.string_to_full_hash(transaction_hash_string)},
         {:ok, %Status{} = status} <- status_of(transaction_hash) do
      conn
      |> put_status(200)
      |> render(:status, %{status: status})
    end
  end

  defp status_of(%Hash{} = transaction_hash) do
    case Chain.hash_to_transaction(transaction_hash, @api_true) do
      {:ok, %{block_number: nil}} ->
        {:ok, Status.of(nil, Finality.heights(@api_true), sequencer_verification(transaction_hash))}

      {:ok, %{block_number: block_number}} ->
        {:ok, Status.of(block_number, Finality.heights(@api_true))}

      {:error, :not_found} ->
        case sequencer_verification(transaction_hash) do
          {:receipted, _key} = verification -> {:ok, Status.of(nil, Finality.heights(@api_true), verification)}
          _unverified -> {:error, :not_found}
        end
    end
  end

  defp sequencer_verification(%Hash{} = transaction_hash) do
    router_url = Application.get_env(:explorer, SequencerReceipt, [])[:router_url]

    with url when is_binary(url) <- router_url,
         {:ok, %{} = answer} <- sequencer_receipt(url, transaction_hash),
         {:ok, receipt_keys} <- SequencerReceipt.receipt_keys(url) do
      SequencerReceipt.verify(transaction_hash, answer, receipt_keys)
    else
      _unavailable -> nil
    end
  end

  defp sequencer_receipt(router_url, %Hash{} = transaction_hash) do
    body =
      Jason.encode!(%{
        jsonrpc: "2.0",
        id: 1,
        method: "pax_sequencerReceipt",
        params: [to_string(transaction_hash)]
      })

    case HttpClient.post(router_url, body, [{"Content-Type", "application/json"}],
           recv_timeout: @router_timeout,
           timeout: @router_timeout
         ) do
      {:ok, %{status_code: 200, body: response}} -> rpc_result(response)
      {:ok, %{status_code: status}} -> {:error, {:http_status, status}}
      {:error, reason} -> {:error, reason}
    end
  end

  defp rpc_result(response) do
    case Jason.decode(response) do
      {:ok, %{"result" => %{} = result}} -> {:ok, result}
      {:ok, %{"result" => nil}} -> {:error, :not_found}
      {:ok, %{"error" => error}} -> {:error, {:rpc_error, error}}
      _ -> {:error, :malformed_response}
    end
  end
end

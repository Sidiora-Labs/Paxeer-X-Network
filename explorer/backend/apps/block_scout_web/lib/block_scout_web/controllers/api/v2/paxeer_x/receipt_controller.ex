defmodule BlockScoutWeb.API.V2.PaxeerX.ReceiptController do
  @moduledoc """
  Publishes the kernel receipts of Paxeer X Network from the public receipt projection.

  Every row carries its provenance class: `kernel` rows were read from a LayerX relay/archive
  batch whose sequencer signature, raw batch digest and receipt digests were checked against
  the pinned network before import, and carry the batch, sequence, activity and proof they were
  verified from; `evm` rows were decoded from receipt logs and carry the transaction, log and
  block that recorded them. Only the projection's public columns are read.
  """

  use BlockScoutWeb, :controller
  use OpenApiSpex.ControllerSpecs

  import BlockScoutWeb.Chain, only: [next_page_params: 5, split_list_by_page: 1]
  import Explorer.PagingOptions, only: [default_paging_options: 0]

  alias BlockScoutWeb.Schemas.API.V2.ErrorResponses.NotFoundResponse
  alias BlockScoutWeb.Schemas.API.V2.PaxeerX.Parameters
  alias Explorer.Chain
  alias Explorer.Chain.{Block, Hash}
  alias Explorer.Chain.PaxeerX.{Receipt, Status, UnifiedAccount}

  action_fallback(BlockScoutWeb.API.V2.FallbackController)

  @api_true [api?: true]

  tags(["paxeer-x"])

  operation :receipts,
    summary: "List the kernel receipts of Paxeer X Network",
    description:
      "Retrieves a paginated list of kernel receipts, newest receipt id first, one row per receipt. " <>
        "Each item carries its origin: `kernel` receipts were verified from a LayerX relay/archive batch " <>
        "and carry its batch, sequence, activity and proof provenance; `evm` receipts were decoded from " <>
        "receipt logs and carry the transaction, log and block that recorded them.",
    parameters:
      base_params() ++
        Parameters.define_paging_params(["receipt_id"]) ++
        define_paging_params(["items_count"]),
    responses: [
      ok:
        {"Kernel receipts with pagination.", "application/json",
         paginated_response(
           items: Schemas.PaxeerX.Receipt,
           next_page_params_example: %{
             "id" => "0x0000000000000000000000000000000000000000000000000000000000000065",
             "items_count" => 50
           }
         )}
    ]

  @doc """
  Handles GET requests to `/api/v2/paxeer-x/receipts`.

  Answers with a page of kernel receipts, newest receipt id first.
  """
  @spec receipts(Plug.Conn.t(), map()) :: Plug.Conn.t()
  def receipts(conn, params) do
    heights = UnifiedAccount.heights(@api_true)

    {receipts, next_page} =
      params
      |> paging_key()
      |> Receipt.public_page(default_paging_options().page_size)
      |> split_list_by_page()

    conn
    |> put_status(200)
    |> json(%{
      "items" => Enum.map(receipts, &item(&1, heights)),
      "next_page_params" => next_page_params(next_page, receipts, params, false, &paging_params/1)
    })
  end

  operation :receipt,
    summary: "Retrieve one kernel receipt of Paxeer X Network",
    description:
      "Retrieves one kernel receipt by its id, as its newest row reports it, with its origin, the " <>
        "verification it carries, its provenance and the rung it has reached on the kernel " <>
        "verification lattice.",
    parameters: [Parameters.receipt_id_param() | base_params()],
    responses: [
      ok: {"The kernel receipt.", "application/json", Schemas.PaxeerX.Receipt.Details},
      not_found: NotFoundResponse.response()
    ]

  @doc """
  Handles GET requests to `/api/v2/paxeer-x/receipts/:id`.
  """
  @spec receipt(Plug.Conn.t(), map()) :: Plug.Conn.t() | {atom(), any()}
  def receipt(conn, %{"id" => id} = _params) do
    with {:ok, receipt_id} <- Hash.Full.cast(id),
         %Receipt{} = receipt <- Receipt.public_get(receipt_id) do
      conn
      |> put_status(200)
      |> json(details(receipt, UnifiedAccount.heights(@api_true)))
    else
      _missing -> {:error, :not_found}
    end
  end

  defp item(%Receipt{} = receipt, heights) do
    %{
      "id" => to_string(receipt.receipt_id),
      "account" => hex(receipt.account),
      "status" => to_string(Status.of(receipt.block_number, heights).rung),
      "block_number" => receipt.block_number,
      "origin" => to_string(receipt.origin),
      "verification" => verification(receipt),
      "provenance" => provenance(receipt)
    }
  end

  defp details(%Receipt{} = receipt, heights) do
    receipt
    |> item(heights)
    |> Map.merge(%{
      "verification_status" => to_string(receipt.status),
      "payload_hash" => hex(receipt.payload_hash),
      "transaction_hash" => hex(receipt.transaction_hash),
      "timestamp" => timestamp(receipt)
    })
  end

  defp verification(%Receipt{origin: :kernel, kernel_verification: verification}), do: to_string(verification)

  defp verification(%Receipt{origin: :evm, status: status}), do: to_string(status)

  defp provenance(%Receipt{origin: :kernel} = receipt) do
    proof = receipt.kernel_proof || %{}

    %{
      "kernel" => %{
        "batch_number" => to_string(receipt.kernel_batch_number),
        "batch_id" => hex(receipt.kernel_batch_id),
        "sequence" => to_string(receipt.kernel_sequence),
        "activity_id" => hex(receipt.kernel_activity_id),
        "result_code" => receipt.kernel_result_code,
        "receipt_sha256" => hex(receipt.kernel_receipt_sha256),
        "canonical_sha256" => hex(receipt.kernel_canonical_sha256),
        "batch_raw_sha256" => hex(receipt.kernel_batch_raw_sha256),
        "state_root" => hex(receipt.kernel_state_root),
        "proof" => %{
          "header_hex" => Map.get(proof, "header_hex"),
          "signature_hex" => Map.get(proof, "signature_hex")
        }
      }
    }
  end

  defp provenance(%Receipt{origin: :evm} = receipt) do
    %{
      "evm" => %{
        "transaction_hash" => hex(receipt.transaction_hash),
        "log_index" => receipt.log_index,
        "block_hash" => hex(receipt.block_hash),
        "block_number" => receipt.block_number
      }
    }
  end

  defp timestamp(%Receipt{origin: :evm, block_hash: %Hash{} = block_hash}) do
    case Chain.hash_to_block(block_hash, @api_true) do
      {:ok, %Block{timestamp: timestamp}} -> timestamp
      _missing -> nil
    end
  end

  defp timestamp(%Receipt{}), do: nil

  defp hex(nil), do: nil

  defp hex(value), do: to_string(value)

  defp paging_key(%{"id" => id}) when is_binary(id) do
    case Hash.Full.cast(id) do
      {:ok, receipt_id} -> receipt_id
      :error -> nil
    end
  end

  defp paging_key(_params), do: nil

  defp paging_params(%Receipt{receipt_id: receipt_id}), do: %{id: to_string(receipt_id)}
end

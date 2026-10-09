defmodule BlockScoutWeb.API.V2.PaxeerX.UnifiedAccountController do
  @moduledoc """
  Publishes the one-account view of the Paxeer X Network for one EVM address.
  """

  use BlockScoutWeb, :controller
  use OpenApiSpex.ControllerSpecs

  import BlockScoutWeb.Chain, only: [split_list_by_page: 1]

  alias BlockScoutWeb.AccessHelper
  alias Explorer.Chain
  alias Explorer.Chain.{Block, Hash}
  alias Explorer.Chain.PaxeerX.UnifiedAccount
  alias Phoenix.Token

  action_fallback(BlockScoutWeb.API.V2.FallbackController)

  @api_true [api?: true]
  @cursor_salt "paxeer-x-unified-account-v1"
  @cursor_max_age 3_600
  @page_size 50

  tags(["paxeer-x"])

  operation :unified,
    summary: "Retrieve the one-account view of an address on Paxeer X Network",
    description:
      "Retrieves the one-account view of an EVM address: the account's four identities, one asset list " <>
        "where each asset carries a single total beside its chain, custody and kernel parts, and one " <>
        "activity feed merging the chain's transactions and token transfers with the LayerX kernel " <>
        "events. Items are ordered by block number, ordinal and kind descending. " <>
        "Follow the account-bound cursor returned in next_page_params. Cursors expire after one hour.",
    parameters:
      [
        address_hash_param(),
        cursor: [
          in: :query,
          schema: %OpenApiSpex.Schema{type: :string, minLength: 1, maxLength: 4096},
          description: "Account-bound activity continuation"
        ]
      ] ++ base_params(),
    responses: [
      ok: {"The one-account view of the address.", "application/json", Schemas.PaxeerX.UnifiedAccount},
      forbidden: ForbiddenResponse.response(),
      unprocessable_entity: {"Invalid parameter(s).", "application/json", message_response_schema()}
    ]

  @doc """
  Handles GET requests to `/api/v2/addresses/:address_hash_param/unified`.

  Answers with the one-account view: the account's four identities, one asset list where each
  asset carries a single total beside its chain, custody and kernel parts, and one activity
  feed merging the chain's transactions and token transfers with the LayerX kernel events.
  """
  @spec unified(Plug.Conn.t(), map()) :: Plug.Conn.t() | {atom(), any()}
  def unified(conn, %{"address_hash_param" => address_hash_string} = params) do
    with {:format, {:ok, address_hash}} <- {:format, Chain.string_to_address_hash(address_hash_string)},
         {:ok, false} <- AccessHelper.restricted_access?(address_hash_string, params) do
      identities = UnifiedAccount.identities(address_hash, @api_true)
      balances = UnifiedAccount.balances(address_hash, identities.kernel_account, @api_true)

      case validated_cursor_state(conn, params, address_hash, identities.kernel_account) do
        {:ok, state} ->
          options = Keyword.put(@api_true, :activity_snapshot_at, DateTime.from_unix!(state.snapshot, :microsecond))

          {activity, next_page} =
            address_hash
            |> UnifiedAccount.activity(identities.kernel_account, state.after, @page_size + 1, options)
            |> split_list_by_page()

          state = initial_state(state, activity)
          page_cursor = sign_cursor(conn, state)
          first_cursor = sign_cursor(conn, %{state | after: state.first, page: 1})

          next_params =
            if next_page == [] do
              nil
            else
              last = List.last(activity)
              %{cursor: sign_cursor(conn, %{state | after: item_key(last), page: state.page + 1})}
            end

          conn
          |> put_status(200)
          |> render(:unified, %{
            identities: identities,
            balances: balances,
            activity: activity,
            next_page_params: next_params,
            page_cursor: page_cursor,
            first_page_cursor: first_cursor,
            page_number: state.page,
            activity_total: if(state.page == 1 and is_nil(next_params), do: length(activity), else: nil)
          })

        {:error, reason} ->
          conn |> put_status(422) |> json(%{message: "Invalid activity cursor", reason: reason})
      end
    end
  end

  defp cursor_state(conn, %{"cursor" => cursor}, address_hash, kernel_account)
       when is_binary(cursor) and byte_size(cursor) <= 4096 do
    with {:ok,
          %{
            version: 1,
            account: account,
            kernel: kernel,
            after: after_key,
            first: first,
            anchor: anchor,
            page: page,
            snapshot: snapshot
          } = state} <-
           Token.verify(conn, @cursor_salt, cursor, max_age: @cursor_max_age),
         true <- account == to_string(address_hash) and kernel == kernel_account,
         true <- valid_key?(after_key) and valid_key?(first) and is_integer(page) and page > 0,
         true <- is_integer(snapshot) and snapshot <= System.system_time(:microsecond),
         true <- System.system_time(:microsecond) - snapshot < @cursor_max_age * 1_000_000,
         true <- anchor_valid?(anchor) do
      {:ok, state}
    else
      {:error, :expired} -> {:error, "expired"}
      _ -> {:error, "malformed, wrong account, changed binding or reorganized snapshot"}
    end
  end

  defp cursor_state(_conn, %{"cursor" => _}, _address, _kernel), do: {:error, "malformed"}

  defp cursor_state(_conn, _params, address_hash, kernel_account) do
    {:ok,
     %{
       version: 1,
       account: to_string(address_hash),
       kernel: kernel_account,
       after: nil,
       first: nil,
       anchor: nil,
       page: 1,
       snapshot: System.system_time(:microsecond)
     }}
  end

  defp validated_cursor_state(conn, params, address_hash, kernel_account) do
    if Enum.any?(["block_number", "index", "items_count"], &Map.has_key?(params, &1)) do
      {:error, "use the returned cursor instead of a partial activity key"}
    else
      cursor_state(conn, params, address_hash, kernel_account)
    end
  end

  defp initial_state(%{first: nil} = state, [item | _]) do
    first = {item.block_number, item.ordinal + 1, ""}
    %{state | after: first, first: first, anchor: to_string(item.block_hash)}
  end

  defp initial_state(state, _activity), do: state

  defp item_key(item), do: {item.block_number, item.ordinal, item.kind}
  defp valid_key?(nil), do: true

  defp valid_key?({block, ordinal, kind}),
    do: is_integer(block) and block >= 0 and is_integer(ordinal) and ordinal >= 0 and is_binary(kind)

  defp valid_key?(_), do: false
  defp anchor_valid?(nil), do: true

  defp anchor_valid?(value) do
    case Hash.Full.cast(value) do
      {:ok, hash} -> not is_nil(Chain.select_repo(@api_true).get_by(Block, hash: hash, consensus: true))
      _ -> false
    end
  end

  defp sign_cursor(conn, state), do: Token.sign(conn, @cursor_salt, state)
end

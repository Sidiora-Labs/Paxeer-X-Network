defmodule BlockScoutWeb.API.V2.PaxeerX.UnifiedAccountController do
  use BlockScoutWeb, :controller
  use OpenApiSpex.ControllerSpecs

  import BlockScoutWeb.Chain, only: [split_list_by_page: 1]

  alias BlockScoutWeb.AccessHelper
  alias Explorer.Chain
  alias Explorer.Chain.{Block, Hash}
  alias Explorer.Chain.PaxeerX.UnifiedAccount
  alias OpenApiSpex.Schema
  alias Phoenix.Token

  action_fallback(BlockScoutWeb.API.V2.FallbackController)

  @api_true [api?: true]
  @cursor_salt "paxeer-x-native-account-v2"
  @cursor_max_age 3_600
  @page_size 50
  @asset_schema %Schema{
    type: :object,
    properties: %{
      id: %Schema{type: :string},
      denom: %Schema{type: :string},
      symbol: %Schema{type: :string, nullable: true},
      decimals: %Schema{type: :integer, minimum: 0, nullable: true}
    },
    required: [:id, :denom, :symbol, :decimals],
    additionalProperties: false
  }
  @native_schema %Schema{
    type: :object,
    properties: %{
      identities: %Schema{
        type: :object,
        properties: %{
          evm: %Schema{type: :string},
          pax: %Schema{type: :string, nullable: true},
          did: %Schema{type: :string, nullable: true}
        },
        required: [:evm, :pax, :did],
        additionalProperties: false
      },
      balances: %Schema{
        type: :array,
        items: %Schema{
          type: :object,
          properties: %{asset: @asset_schema, total: %Schema{type: :string}},
          required: [:asset, :total],
          additionalProperties: false
        }
      },
      activity: %Schema{
        type: :array,
        items: %Schema{
          type: :object,
          properties: %{
            kind: %Schema{type: :string, enum: ["transaction"]},
            hash: %Schema{type: :string},
            block_number: %Schema{type: :integer, minimum: 0},
            ordinal: %Schema{type: :integer, minimum: 0},
            status: %Schema{type: :string, enum: ["committed"]},
            result: %Schema{type: :string, enum: ["ok", "error"], nullable: true},
            timestamp: %Schema{type: :string, format: :"date-time"},
            asset: @asset_schema,
            amount: %Schema{type: :string},
            counterparty: %Schema{type: :string, nullable: true}
          },
          required: [
            :kind,
            :hash,
            :block_number,
            :ordinal,
            :status,
            :result,
            :timestamp,
            :asset,
            :amount,
            :counterparty
          ],
          additionalProperties: false
        }
      },
      next_page_params: %Schema{
        type: :object,
        nullable: true,
        properties: %{cursor: %Schema{type: :string}},
        required: [:cursor],
        additionalProperties: false
      },
      page_cursor: %Schema{type: :string},
      first_page_cursor: %Schema{type: :string},
      page_number: %Schema{type: :integer, minimum: 1},
      activity_total: %Schema{type: :integer, minimum: 0, nullable: true}
    },
    required: [
      :identities,
      :balances,
      :activity,
      :next_page_params,
      :page_cursor,
      :first_page_cursor,
      :page_number,
      :activity_total
    ],
    additionalProperties: false
  }
  @invalid_response_schema %Schema{
    oneOf: [
      %Schema{
        type: :object,
        properties: %{message: %Schema{type: :string, enum: ["Invalid parameter(s)"]}},
        required: [:message],
        additionalProperties: false
      },
      %Schema{
        type: :object,
        properties: %{
          message: %Schema{type: :string, enum: ["Invalid activity cursor"]},
          reason: %Schema{
            type: :string,
            enum: [
              "expired",
              "malformed",
              "malformed, wrong account, changed binding or reorganized snapshot",
              "use the returned cursor instead of a partial activity key",
              "invalid_unix_time"
            ]
          }
        },
        required: [:message, :reason],
        additionalProperties: false
      }
    ]
  }

  tags(["paxeer-x"])

  operation :unified,
    summary: "Retrieve the native address page on Paxeer X Network",
    description:
      "Retrieves the address's EVM, Paxeer and DID identities, bank and ERC-20 balances, " <>
        "and native transactions ordered by block number and transaction index descending. " <>
        "Follow the account-bound cursor returned in next_page_params. Cursors expire after one hour.",
    parameters:
      [
        address_hash_param(),
        cursor: [
          in: :query,
          schema: %Schema{type: :string, minLength: 1, maxLength: 4096},
          description: "Account-bound activity continuation"
        ]
      ] ++ base_params(),
    responses: [
      ok: {"The native address page.", "application/json", @native_schema},
      forbidden: ForbiddenResponse.response(),
      unprocessable_entity: {"Invalid parameter(s).", "application/json", @invalid_response_schema}
    ]

  @spec unified(Plug.Conn.t(), map()) :: Plug.Conn.t() | {atom(), any()}
  def unified(conn, %{"address_hash_param" => address_hash_string} = params) do
    with {:format, {:ok, address_hash}} <- {:format, Chain.string_to_address_hash(address_hash_string)},
         {:ok, false} <- AccessHelper.restricted_access?(address_hash_string, params) do
      identities = UnifiedAccount.identities(address_hash, @api_true)
      native_page(conn, params, address_hash, identities)
    end
  end

  defp native_page(conn, params, address_hash, identities) do
    with {:ok, state} <- validated_cursor_state(conn, params, address_hash, identities.did),
         {:ok, snapshot} <- DateTime.from_unix(state.snapshot, :microsecond) do
      options = Keyword.put(@api_true, :activity_snapshot_at, snapshot)

      {activity, next_page} =
        address_hash
        |> UnifiedAccount.activity(state.after, @page_size + 1, options)
        |> split_list_by_page()

      state = initial_state(state, activity)
      next_params = next_params(conn, state, activity, next_page)

      conn
      |> put_status(200)
      |> json(%{
        identities: Map.take(identities, [:evm, :pax, :did]),
        balances: Enum.map(UnifiedAccount.balances(address_hash, @api_true), &prepare_balance/1),
        activity: Enum.map(activity, &prepare_activity/1),
        next_page_params: next_params,
        page_cursor: sign_cursor(conn, state),
        first_page_cursor: sign_cursor(conn, %{state | after: state.first, page: 1}),
        page_number: state.page,
        activity_total: if(state.page == 1 and is_nil(next_params), do: length(activity), else: nil)
      })
    else
      {:error, reason} ->
        conn |> put_status(422) |> json(%{message: "Invalid activity cursor", reason: to_string(reason)})
    end
  end

  defp next_params(_conn, _state, _activity, []), do: nil

  defp next_params(conn, state, activity, _next_page) do
    %{cursor: sign_cursor(conn, %{state | after: item_key(List.last(activity)), page: state.page + 1})}
  end

  defp prepare_balance(balance), do: %{asset: balance.asset, total: to_string(balance.total)}

  defp prepare_activity(item) do
    item
    |> Map.drop([:block_hash])
    |> Map.update!(:amount, &to_string/1)
    |> Map.update!(:status, &to_string/1)
    |> Map.update!(:result, fn
      nil -> nil
      result -> to_string(result)
    end)
  end

  defp cursor_state(conn, %{"cursor" => cursor}, address_hash, did)
       when is_binary(cursor) and byte_size(cursor) <= 4096 do
    with {:ok,
          %{
            version: 2,
            account: account,
            did: bound_did,
            after: after_key,
            first: first,
            anchor: anchor,
            page: page,
            snapshot: snapshot
          } = state} <-
           Token.verify(conn, @cursor_salt, cursor, max_age: @cursor_max_age),
         true <- account == to_string(address_hash) and bound_did == did,
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

  defp cursor_state(_conn, %{"cursor" => _}, _address, _did), do: {:error, "malformed"}

  defp cursor_state(_conn, _params, address_hash, did) do
    {:ok,
     %{
       version: 2,
       account: to_string(address_hash),
       did: did,
       after: nil,
       first: nil,
       anchor: nil,
       page: 1,
       snapshot: System.system_time(:microsecond)
     }}
  end

  defp validated_cursor_state(conn, params, address_hash, did) do
    if Enum.any?(["block_number", "index", "items_count"], &Map.has_key?(params, &1)) do
      {:error, "use the returned cursor instead of a partial activity key"}
    else
      cursor_state(conn, params, address_hash, did)
    end
  end

  defp initial_state(%{first: nil} = state, [item | _]) do
    first = {item.block_number, item.ordinal + 1}
    %{state | after: first, first: first, anchor: to_string(item.block_hash)}
  end

  defp initial_state(state, _activity), do: state

  defp item_key(item), do: {item.block_number, item.ordinal}
  defp valid_key?(nil), do: true

  defp valid_key?({block, ordinal}),
    do: is_integer(block) and block >= 0 and is_integer(ordinal) and ordinal >= 0

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

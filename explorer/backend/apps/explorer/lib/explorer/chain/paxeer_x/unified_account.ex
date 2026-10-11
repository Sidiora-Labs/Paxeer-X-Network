defmodule Explorer.Chain.PaxeerX.UnifiedAccount do
  import Ecto.Query
  import Explorer.Chain, only: [select_repo: 1]

  alias Explorer.Chain
  alias Explorer.Chain.Address
  alias Explorer.Chain.Address.CurrentTokenBalance
  alias Explorer.Chain.{Hash, Token, Transaction, Wei}
  alias Explorer.Chain.PaxeerX.{AccountBinding, Identity}

  @native_asset_id "native"
  @native_decimals 18

  @type identities :: %{
          evm: String.t(),
          pax: String.t() | nil,
          did: String.t() | nil,
          bound: boolean()
        }

  @type asset_ref :: %{
          id: String.t(),
          denom: String.t(),
          symbol: String.t() | nil,
          decimals: non_neg_integer() | nil
        }

  @type asset :: %{asset: asset_ref(), total: Decimal.t()}

  @type activity_item :: %{
          kind: String.t(),
          hash: String.t(),
          block_number: non_neg_integer(),
          block_hash: Hash.Full.t(),
          ordinal: non_neg_integer(),
          timestamp: DateTime.t(),
          asset: asset_ref(),
          amount: Decimal.t(),
          counterparty: String.t() | nil,
          status: :committed,
          result: :ok | :error | nil
        }

  @spec identities(Hash.Address.t(), keyword()) :: identities()
  def identities(%Hash{} = address_hash, options \\ []) do
    evm = Address.checksum(address_hash)
    query = latest_binding_query(dynamic([binding], binding.evm_address_hash == ^address_hash))

    case select_repo(options).one(query) do
      %AccountBinding{bound: true} = binding ->
        %{evm: evm, pax: binding.pax_address, did: binding.layerx_did, bound: true}

      _unbound ->
        %{evm: evm, pax: nil, did: nil, bound: false}
    end
  end

  @spec resolve(Identity.t(), keyword()) :: {:ok, Hash.Address.t()} | :error
  def resolve(%Identity{kind: :evm, evm: evm}, _options), do: {:ok, evm}

  def resolve(%Identity{} = identity, options) do
    case identity |> binding_condition() |> latest_binding_query() |> select_repo(options).one() do
      %AccountBinding{bound: true, evm_address_hash: %Hash{} = address_hash} -> {:ok, address_hash}
      _unbound -> :error
    end
  end

  @spec balances(Hash.Address.t(), keyword()) :: [asset()]
  def balances(%Hash{} = address_hash, options \\ []) do
    coin =
      case Chain.hash_to_address(address_hash, Keyword.put(options, :necessity_by_association, %{})) do
        {:ok, address} -> address.fetched_coin_balance || %Wei{value: Decimal.new(0)}
        {:error, :not_found} -> %Wei{value: Decimal.new(0)}
      end

    tokens =
      address_hash
      |> CurrentTokenBalance.last_token_balances(["ERC-20"])
      |> select_repo(options).all()
      |> Enum.map(fn balance ->
        id = Address.checksum(balance.token_contract_address_hash)
        %{asset: token_asset_ref(id, balance.token), total: balance.value}
      end)

    [%{asset: native_asset_ref(), total: coin.value} | tokens]
    |> Enum.sort_by(& &1.asset.id)
  end

  @spec activity(Hash.Address.t(), {non_neg_integer(), non_neg_integer()} | nil, pos_integer(), keyword()) ::
          [activity_item()]
  def activity(%Hash{} = address_hash, paging_key, limit, options \\ []) when is_integer(limit) and limit > 0 do
    Transaction
    |> where(
      [transaction],
      transaction.from_address_hash == ^address_hash or transaction.to_address_hash == ^address_hash or
        transaction.created_contract_address_hash == ^address_hash
    )
    |> join(:inner, [transaction], block in assoc(transaction, :block), as: :block)
    |> where([transaction, block: block], block.consensus == true and not is_nil(transaction.index))
    |> activity_snapshot(options)
    |> page_activity(paging_key)
    |> order_by([transaction], desc: transaction.block_number, desc: transaction.index)
    |> limit(^limit)
    |> select([transaction, block: block], %{
      block_number: transaction.block_number,
      block_hash: transaction.block_hash,
      ordinal: transaction.index,
      timestamp: block.timestamp,
      hash: transaction.hash,
      from: transaction.from_address_hash,
      to: transaction.to_address_hash,
      amount: transaction.value,
      result: transaction.status
    })
    |> select_repo(options).all()
    |> Enum.map(fn item ->
      other = if item.from == address_hash, do: item.to, else: item.from

      item
      |> Map.drop([:from, :to])
      |> Map.merge(%{
        kind: "transaction",
        hash: to_string(item.hash),
        asset: native_asset_ref(),
        amount: item.amount.value,
        counterparty: other && Address.checksum(other),
        status: :committed
      })
    end)
  end

  defp latest_binding_query(condition) do
    from(binding in AccountBinding.only_consensus_query(),
      where: ^condition,
      order_by: [desc: binding.block_number, desc: binding.log_index],
      limit: 1
    )
  end

  defp binding_condition(%Identity{kind: :pax, pax: pax}), do: dynamic([binding], binding.pax_address == ^pax)
  defp binding_condition(%Identity{kind: :did, did: did}), do: kernel_binding_condition(did)

  defp binding_condition(%Identity{kind: :kernel_account, kernel_account: kernel_account}),
    do: kernel_binding_condition(kernel_account)

  defp kernel_binding_condition(account) do
    candidates = account_candidates(account)
    dynamic([binding], binding.layerx_did in ^candidates or binding.layerx_account in ^candidates)
  end

  defp token_asset_ref(id, %Token{} = token) do
    %{id: id, denom: token.symbol || id, symbol: token.symbol, decimals: decimals(token.decimals)}
  end

  defp native_asset_ref do
    %{id: @native_asset_id, denom: Explorer.coin(), symbol: Explorer.coin(), decimals: @native_decimals}
  end

  defp decimals(nil), do: nil
  defp decimals(%Decimal{} = value), do: Decimal.to_integer(value)

  defp activity_snapshot(query, options) do
    case Keyword.get(options, :activity_snapshot_at) do
      nil -> query
      timestamp -> where(query, [item], item.inserted_at <= ^timestamp)
    end
  end

  defp page_activity(query, nil), do: query

  defp page_activity(query, {block_number, ordinal}) do
    where(
      query,
      [item],
      item.block_number < ^block_number or
        (item.block_number == ^block_number and item.index < ^ordinal)
    )
  end

  defp account_candidates(nil), do: []

  defp account_candidates(account) do
    case Identity.key(account) do
      nil -> [account]
      key -> Enum.uniq([account, key, Identity.kernel_account(key), Identity.did(key)])
    end
  end
end

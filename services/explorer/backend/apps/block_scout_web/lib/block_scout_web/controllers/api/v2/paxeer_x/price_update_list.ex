defmodule BlockScoutWeb.API.V2.PaxeerX.PriceUpdateList do
  @moduledoc """
  Interleaves native price-feed submissions with EVM transactions in the transaction lists.

  Both kinds are keyset paged on `(block_number, index)`. A submission pages at its CometBFT
  index plus 1_000_000, so a page boundary is unique whichever kind ends a page: in a
  block, every submission sorts above every EVM transaction.
  """

  import Explorer.MicroserviceInterfaces.BENS, only: [maybe_preload_ens: 1]
  import Explorer.MicroserviceInterfaces.Metadata, only: [maybe_preload_metadata: 1]

  alias BlockScoutWeb.API.V2.PaxeerX.FeedView
  alias BlockScoutWeb.API.V2.TransactionView
  alias Explorer.Chain
  alias Explorer.Chain.{Block, Transaction}
  alias Explorer.Chain.PaxeerX.{PriceFeed, PriceUpdate}
  alias Explorer.PagingOptions

  @index_offset 1_000_000

  @doc """
  Splits `:price_update` out of a transaction type filter.

  Returns the EVM types to filter by, whether EVM transactions are listed at all and whether
  submissions are listed: no filter lists both, `price_update` alone lists only submissions.
  """
  @spec split_types([atom()]) :: {[atom()], boolean(), boolean()}
  def split_types(types) do
    case Enum.split_with(types, &(&1 == :price_update)) do
      {[], []} -> {[], true, true}
      {_updates, []} -> {[], false, true}
      {updates, evm_types} -> {evm_types, true, updates != []}
    end
  end

  @doc """
  The newest consensus submissions below the paging key, at most the page size of `paging_options`.
  """
  @spec recent_updates(PagingOptions.t()) :: [PriceUpdate.t()]
  def recent_updates(%PagingOptions{key: key} = paging_options) do
    PriceFeed.list_updates(%PagingOptions{paging_options | key: update_key(key)})
  end

  @doc """
  The submissions of a consensus block above the paging key, in transaction order.
  """
  @spec block_updates(Block.t(), PagingOptions.t()) :: [PriceUpdate.t()]
  def block_updates(%Block{consensus: true, number: number}, %PagingOptions{key: key}) do
    number
    |> PriceFeed.block_updates()
    |> Enum.reverse()
    |> Enum.filter(&above_key?(&1, key))
  end

  def block_updates(%Block{}, %PagingOptions{}), do: []

  @doc """
  Merges two lists into one page ordered by paging position, newest first or oldest first.
  """
  @spec merge([Transaction.t() | PriceUpdate.t()], [PriceUpdate.t()], :desc | :asc, pos_integer()) ::
          [Transaction.t() | PriceUpdate.t()]
  def merge(transactions, updates, order, page_size) do
    (transactions ++ updates)
    |> Enum.sort_by(&position/1, order)
    |> Enum.take(page_size)
  end

  @doc """
  The paging parameters of the item that ends a page.
  """
  @spec paging_params(Transaction.t() | PriceUpdate.t()) :: %{block_number: non_neg_integer(), index: integer()}
  def paging_params(item) do
    {block_number, index} = position(item)
    %{block_number: block_number, index: index}
  end

  @doc """
  Renders a page that holds submissions as transaction list items in page order. EVM
  transactions render exactly as the transaction view renders them.
  """
  @spec render_items(Plug.Conn.t(), [Transaction.t() | PriceUpdate.t()]) :: [map()]
  def render_items(conn, page) do
    rendered =
      case Enum.filter(page, &match?(%Transaction{}, &1)) do
        [] ->
          []

        transactions ->
          TransactionView.render("transactions.json", %{
            transactions: transactions |> maybe_preload_ens() |> maybe_preload_metadata(),
            conn: conn
          })
      end

    symbols = PriceFeed.market_symbols()
    block_height = Chain.block_height(api?: true)

    {items, []} =
      Enum.map_reduce(page, rendered, fn
        %Transaction{}, [item | rest] -> {item, rest}
        %PriceUpdate{} = update, rest -> {FeedView.prepare_update(update, symbols, block_height), rest}
      end)

    items
  end

  defp position(%Transaction{block_number: block_number, index: index}), do: {block_number, index}
  defp position(%PriceUpdate{block_number: block_number, index: index}), do: {block_number, index + @index_offset}

  defp update_key({block_number, index}) when is_integer(block_number) and is_integer(index),
    do: {block_number, index - @index_offset}

  defp update_key(_key), do: nil

  defp above_key?(update, {_block_number, index}) when is_integer(index), do: elem(position(update), 1) > index
  defp above_key?(_update, _key), do: true
end

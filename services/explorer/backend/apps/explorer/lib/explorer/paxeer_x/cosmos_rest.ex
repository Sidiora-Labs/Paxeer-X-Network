defmodule Explorer.PaxeerX.CosmosRest do
  @moduledoc """
  Reads the native price-feed submissions of one block from a node.

  The transactions come from the Cosmos REST route `GET /cosmos/tx/v1beta1/txs/block/{height}`,
  which carries each decoded transaction and the raw transaction bytes of the block but no
  execution result; a block without transactions answers 400 with `cannot paginate 0 txs`,
  which reads as an empty block. The execution result of each transaction - its code and
  gas used, both omitted when zero - comes from the CometBFT route
  `GET /block_results?height={height}`, which is read only when the block carries a
  submission.

  Every request tries the configured URLs in order and goes round them at most
  `@rounds` times; the last failure is returned as a typed error.
  """

  alias Explorer.Chain.PaxeerX.Identity

  @submit_prices_type "/paxprotocol.paxchain.pricefeed.MsgSubmitPrices"
  @page_limit 100
  @rounds 2
  @retry_delay_ms 200
  @max_body_bytes 33_554_432
  @http_options [recv_timeout: 30_000, timeout: 10_000, follow_redirect: false]
  @max_int64 9_223_372_036_854_775_807
  @max_int32 2_147_483_647

  @type error ::
          :no_urls
          | {:unreachable, String.t()}
          | {:http_status, non_neg_integer(), String.t()}
          | {:too_large, String.t()}
          | {:malformed, String.t()}

  @type observation :: %{
          market_id: non_neg_integer(),
          sequence: non_neg_integer(),
          index_price: non_neg_integer(),
          mark_price: non_neg_integer(),
          source_timestamp_ms: non_neg_integer()
        }

  @type price_update :: %{
          hash: String.t(),
          index: non_neg_integer(),
          feeder_bech32: String.t(),
          feeder_hash: String.t(),
          status: 0 | 1,
          gas_used: non_neg_integer(),
          gas_wanted: non_neg_integer(),
          observations: [observation()]
        }

  @doc """
  The price-feed submissions of the block at `height`, in CometBFT transaction order.
  """
  @spec block_price_updates([String.t()], [String.t()], non_neg_integer()) ::
          {:ok, [price_update()]} | {:error, error()}
  def block_price_updates(rest_urls, rpc_urls, height) when is_integer(height) and height >= 0 do
    with {:ok, block_txs} <- fetch_block_txs(rest_urls, height),
         {:ok, submissions} <- decode_block_txs(block_txs) do
      case submissions do
        [] ->
          {:ok, []}

        _ ->
          with {:ok, results} <- fetch_block_results(rpc_urls, height) do
            decode_price_updates(submissions, results, length(block_txs["raw"]))
          end
      end
    end
  end

  @doc """
  Every page of the block's transactions merged into `%{"txs" => [...], "raw" => [...]}`,
  where `raw` holds the base64 raw transaction bytes of the block.
  """
  @spec fetch_block_txs([String.t()], non_neg_integer()) :: {:ok, map()} | {:error, error()}
  def fetch_block_txs(urls, height), do: fetch_block_txs(urls, height, 0, [], nil)

  defp fetch_block_txs(urls, height, offset, txs, raw) do
    path = "/cosmos/tx/v1beta1/txs/block/#{height}?pagination.offset=#{offset}&pagination.limit=#{@page_limit}"

    case get_json(urls, path, &empty_block?/2) do
      {:ok, :empty} when offset == 0 ->
        {:ok, %{"txs" => [], "raw" => []}}

      {:ok, %{"txs" => page, "block" => %{"data" => %{"txs" => block_raw}}} = body}
      when is_list(page) and is_list(block_raw) ->
        collected = txs ++ page
        total = total(body, block_raw)

        cond do
          length(collected) == total and total == length(block_raw) ->
            {:ok, %{"txs" => collected, "raw" => raw || block_raw}}

          page != [] and length(collected) < total ->
            fetch_block_txs(urls, height, offset + length(page), collected, raw || block_raw)

          true ->
            {:error, {:malformed, "transaction count does not match the block"}}
        end

      {:ok, _} ->
        {:error, {:malformed, "unexpected block transactions response"}}

      error ->
        error
    end
  end

  defp total(%{"pagination" => %{"total" => total}}, _block_raw) when is_binary(total) do
    case Integer.parse(total) do
      {value, ""} -> value
      _ -> -1
    end
  end

  defp total(_body, block_raw), do: length(block_raw)

  defp empty_block?(400, %{"message" => message}) when is_binary(message),
    do: String.contains?(message, "cannot paginate 0 txs")

  defp empty_block?(_status, _body), do: false

  @doc """
  The CometBFT execution results of the block's transactions, in transaction order.
  """
  @spec fetch_block_results([String.t()], non_neg_integer()) :: {:ok, [map()]} | {:error, error()}
  def fetch_block_results(urls, height) do
    case get_json(urls, "/block_results?height=#{height}", fn _, _ -> false end) do
      {:ok, %{"result" => %{} = result}} -> results(result)
      {:ok, %{} = result} -> results(result)
      error -> error
    end
  end

  defp results(%{"txs_results" => nil}), do: {:ok, []}
  defp results(%{"txs_results" => results}) when is_list(results), do: {:ok, results}
  defp results(_), do: {:error, {:malformed, "unexpected block results response"}}

  @doc """
  The price-feed submissions among merged block transactions, each with its index, its
  hash and its declared gas limit, before the execution results are joined in.
  """
  @spec decode_block_txs(map()) :: {:ok, [map()]} | {:error, error()}
  def decode_block_txs(%{"txs" => txs, "raw" => raw}) do
    txs
    |> Enum.with_index()
    |> Enum.reduce_while({:ok, []}, fn {tx, index}, {:ok, acc} ->
      case decode_tx(tx, Enum.at(raw, index), index) do
        {:ok, nil} -> {:cont, {:ok, acc}}
        {:ok, submission} -> {:cont, {:ok, [submission | acc]}}
        {:error, _} = error -> {:halt, error}
      end
    end)
    |> case do
      {:ok, submissions} -> {:ok, Enum.reverse(submissions)}
      error -> error
    end
  end

  defp decode_tx(%{"body" => %{"messages" => messages}} = tx, raw, index) when is_list(messages) do
    case Enum.filter(messages, &match?(%{"@type" => @submit_prices_type}, &1)) do
      [] ->
        {:ok, nil}

      [%{"feeder" => feeder} | _] = submissions ->
        with {:ok, hash} <- tx_hash(raw),
             {:ok, feeder_bytes} <- feeder(feeder),
             true <- Enum.all?(submissions, &(&1["feeder"] == feeder)) || {:error, {:malformed, "mixed feeders"}},
             {:ok, gas_wanted} <- integer(get_in(tx, ["auth_info", "fee", "gas_limit"]) || "0", @max_int64),
             {:ok, observations} <- observations(Enum.flat_map(submissions, &List.wrap(&1["observations"]))) do
          {:ok,
           %{
             hash: hash,
             index: index,
             feeder_bech32: feeder,
             feeder_hash: "0x" <> Base.encode16(feeder_bytes, case: :lower),
             gas_wanted: gas_wanted,
             observations: observations
           }}
        end

      _ ->
        {:error, {:malformed, "submission without a feeder"}}
    end
  end

  defp decode_tx(_tx, _raw, _index), do: {:error, {:malformed, "transaction without a body"}}

  defp tx_hash(raw) when is_binary(raw) do
    case Base.decode64(raw) do
      {:ok, bytes} -> {:ok, "0x" <> Base.encode16(:crypto.hash(:sha256, bytes), case: :lower)}
      :error -> {:error, {:malformed, "raw transaction is not base64"}}
    end
  end

  defp tx_hash(_raw), do: {:error, {:malformed, "missing raw transaction"}}

  defp feeder(feeder) when is_binary(feeder) do
    case Identity.pax_address_bytes(feeder) do
      {:ok, bytes} -> {:ok, bytes}
      :error -> {:error, {:malformed, "feeder is not a pax account"}}
    end
  end

  defp feeder(_feeder), do: {:error, {:malformed, "feeder is not a pax account"}}

  defp observations(observations) do
    observations
    |> Enum.reduce_while({:ok, %{}}, fn observation, {:ok, acc} ->
      case observation(observation) do
        {:ok, decoded} -> {:cont, {:ok, Map.put(acc, decoded.market_id, decoded)}}
        error -> {:halt, error}
      end
    end)
    |> case do
      {:ok, by_market} -> {:ok, by_market |> Map.values() |> Enum.sort_by(& &1.market_id)}
      error -> error
    end
  end

  defp observation(%{} = observation) do
    with {:ok, market_id} <- integer(observation["market_id"], @max_int32),
         {:ok, sequence} <- integer(observation["sequence"], @max_int64),
         {:ok, index_price} <- price(observation["index_price"]),
         {:ok, mark_price} <- price(observation["mark_price"]),
         {:ok, source_timestamp_ms} <- integer(observation["source_timestamp_ms"], @max_int64) do
      {:ok,
       %{
         market_id: market_id,
         sequence: sequence,
         index_price: index_price,
         mark_price: mark_price,
         source_timestamp_ms: source_timestamp_ms
       }}
    end
  end

  defp observation(_observation), do: {:error, {:malformed, "observation is not an object"}}

  defp price(value) when is_binary(value) and byte_size(value) <= 78, do: integer(value, nil)
  defp price(_value), do: {:error, {:malformed, "price is not an 18-decimal integer"}}

  defp integer(value, max) when is_binary(value) do
    if value =~ ~r/\A(0|[1-9][0-9]*)\z/ and (is_nil(max) or String.to_integer(value) <= max) do
      {:ok, String.to_integer(value)}
    else
      {:error, {:malformed, "not an unsigned integer in range: #{inspect(value)}"}}
    end
  end

  defp integer(value, max) when is_integer(value) and value >= 0 and (is_nil(max) or value <= max), do: {:ok, value}
  defp integer(value, _max), do: {:error, {:malformed, "not an unsigned integer in range: #{inspect(value)}"}}

  @doc """
  Joins the execution result at each submission's index: status 1 for code 0, else 0,
  and the gas used.
  """
  @spec decode_price_updates([map()], [map()], non_neg_integer()) :: {:ok, [price_update()]} | {:error, error()}
  def decode_price_updates(submissions, results, transaction_count) do
    if length(results) == transaction_count do
      submissions
      |> Enum.reduce_while({:ok, []}, fn submission, {:ok, acc} ->
        result = Enum.at(results, submission.index)

        with %{} <- result,
             {:ok, code} <- integer(Map.get(result, "code", 0), nil),
             {:ok, gas_used} <- integer(Map.get(result, "gas_used", "0"), @max_int64) do
          {:cont, {:ok, [Map.merge(submission, %{status: if(code == 0, do: 1, else: 0), gas_used: gas_used}) | acc]}}
        else
          {:error, _} = error -> {:halt, error}
          _ -> {:halt, {:error, {:malformed, "execution result is not an object"}}}
        end
      end)
      |> case do
        {:ok, updates} -> {:ok, Enum.reverse(updates)}
        error -> error
      end
    else
      {:error, {:malformed, "execution results do not match the block transactions"}}
    end
  end

  defp get_json([], _path, _accept?), do: {:error, :no_urls}

  defp get_json(urls, path, accept?) do
    attempts = for _round <- 1..@rounds, url <- urls, do: url

    attempts
    |> Enum.with_index()
    |> Enum.reduce_while({:error, :no_urls}, fn {url, attempt}, _last ->
      if attempt >= length(urls), do: Process.sleep(@retry_delay_ms)

      case get_once(url, path, accept?) do
        {:ok, _} = ok -> {:halt, ok}
        error -> {:cont, error}
      end
    end)
  end

  defp get_once(url, path, accept?) do
    case HTTPoison.get(url <> path, [{"accept", "application/json"}], @http_options) do
      {:ok, %HTTPoison.Response{body: body}} when byte_size(body) > @max_body_bytes ->
        {:error, {:too_large, url}}

      {:ok, %HTTPoison.Response{status_code: status, body: body}} ->
        case {status, Jason.decode(body)} do
          {200, {:ok, %{} = decoded}} ->
            {:ok, decoded}

          {_, {:ok, %{} = decoded}} ->
            if accept?.(status, decoded), do: {:ok, :empty}, else: {:error, {:http_status, status, url}}

          {200, _} ->
            {:error, {:malformed, "response from #{url} is not a JSON object"}}

          _ ->
            {:error, {:http_status, status, url}}
        end

      {:error, %HTTPoison.Error{}} ->
        {:error, {:unreachable, url}}
    end
  end
end

defmodule Indexer.Fetcher.PaxeerXKernelReceipts do
  @moduledoc """
  Projects kernel receipts from a LayerX relay/archive into `lx_receipts`.

  The worker reads only the public relay/archive routes: `/v1/sync/network`, the
  `/v1/history/batches` listing, `/v1/history/batches/:n` and the raw canonical batch at
  `/v1/sync/batches/:n`. A batch is imported only when the network document matches the
  pinned network and sequencer key, the batch lies inside the authorized range, the raw
  canonical bytes hash to the published digest and carry the published header and
  signature, and every activity receipt and canonical encoding hashes to its published
  digest within the batch sequence range. Otherwise no row of the batch is written, the
  cursor stays where it was and the refusal is recorded on the cursor row.

  Progress is kept in `lx_kernel_receipt_cursors` per relay source, so the worker resumes
  after a restart; rows are keyed by batch number and sequence, so a replay imports
  nothing twice.
  """

  use GenServer

  require Logger

  alias Explorer.{Chain, Repo}
  alias Explorer.Chain.PaxeerX.Receipt.KernelCursor

  @default_interval 2_000
  @http_options [recv_timeout: 30_000, timeout: 10_000, follow_redirect: false]

  @type config :: %{
          relay_url: String.t(),
          network_id: non_neg_integer(),
          sequencer_public_key: String.t(),
          interval: pos_integer()
        }

  def child_spec([config]), do: child_spec([config, []])

  def child_spec([_config, _gen_server_options] = start_link_arguments) do
    %{id: __MODULE__, start: {__MODULE__, :start_link, start_link_arguments}}
  end

  def start_link(config, gen_server_options \\ []) do
    GenServer.start_link(__MODULE__, config, Keyword.put_new(gen_server_options, :name, __MODULE__))
  end

  @doc """
  The worker configuration from the environment, or `nil` when no relay is configured.

  A configured relay without its network and sequencer pins is a configuration error.
  """
  @spec config_from_env() :: config() | nil
  def config_from_env do
    case System.get_env("INDEXER_PAXEER_X_KERNEL_RECEIPTS_RELAY_URL") do
      url when url in [nil, ""] ->
        nil

      url ->
        %{
          relay_url: String.trim_trailing(url, "/"),
          network_id: env_integer!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_NETWORK_ID", nil),
          sequencer_public_key: env_hex!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_SEQUENCER_PUBLIC_KEY"),
          interval: env_integer!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_INTERVAL_MS", @default_interval)
        }
    end
  end

  @impl GenServer
  def init(config) do
    Logger.metadata(fetcher: :paxeer_x_kernel_receipts)
    send(self(), :sync)
    {:ok, config}
  end

  @impl GenServer
  def handle_info(:sync, config) do
    case sync_once(config) do
      {:ok, last_batch} ->
        Logger.debug(fn -> "kernel receipts synchronized through batch #{last_batch}" end)

      {:error, code, batch} ->
        Logger.error("kernel receipt evidence refused: #{code} at batch #{inspect(batch)}")
    end

    Process.send_after(self(), :sync, config.interval)
    {:noreply, config}
  end

  @doc """
  One synchronization pass, the same path the periodic tick takes.
  """
  @spec sync_once(config()) :: {:ok, non_neg_integer()} | {:error, String.t(), non_neg_integer() | nil}
  def sync_once(config) do
    cursor = load_cursor(config.relay_url)

    with {:ok, network} <- get_json(config, "/v1/sync/network"),
         {:ok, range} <- authorize_network(network, config) do
      walk(config, range, cursor)
    else
      {:error, code} -> refuse(config.relay_url, cursor, code, nil)
      {:error, code, batch} -> refuse(config.relay_url, cursor, code, batch)
    end
  end

  defp walk(config, range, cursor) do
    path =
      case cursor.next_cursor do
        nil -> "/v1/history/batches"
        page -> "/v1/history/batches?cursor=" <> URI.encode_www_form(page)
      end

    with {:ok, %{"items" => items} = page} when is_list(items) <- get_json(config, path),
         {:ok, cursor} <- import_page(config, range, cursor, items) do
      case page["next_cursor"] do
        next when is_binary(next) and next != "" ->
          walk(config, range, save_cursor(cursor, %{next_cursor: next}))

        _ ->
          {:ok, cursor.last_batch}
      end
    else
      {:ok, _} -> refuse(config.relay_url, cursor, "malformed", nil)
      {:error, code} -> refuse(config.relay_url, cursor, code, nil)
      {:error, code, batch} -> refuse(config.relay_url, cursor, code, batch)
    end
  end

  defp import_page(config, range, cursor, items) do
    Enum.reduce_while(items, {:ok, cursor}, fn item, {:ok, cursor} ->
      with {:ok, number} <- decimal(item["batch_number"]) do
        if number <= cursor.last_batch do
          {:cont, {:ok, cursor}}
        else
          case import_batch(config, range, number) do
            :ok -> {:cont, {:ok, save_cursor(cursor, %{last_batch: number, refusal_code: nil, refused_batch: nil})}}
            {:error, code} -> {:halt, {:error, code, number}}
          end
        end
      else
        _ -> {:halt, {:error, "malformed", nil}}
      end
    end)
  end

  defp import_batch(config, {first, last}, number) do
    with true <- (number >= first and number <= last) || {:error, "batch_unauthorized"},
         {:ok, batch} <- get_json(config, "/v1/history/batches/#{number}"),
         {:ok, raw} <- get_raw(config, "/v1/sync/batches/#{number}"),
         {:ok, rows} <- verify_batch(batch, raw, number) do
      import_rows(rows)
    end
  end

  @doc false
  @spec verify_batch(map(), binary(), non_neg_integer()) :: {:ok, [map()]} | {:error, String.t()}
  def verify_batch(batch, raw, number) do
    with {:ok, ^number} <- decimal(batch["batch_number"]),
         {:ok, batch_id} <- hex32(batch["batch_id"]),
         {:ok, state_root} <- hex32(batch["resulting_state_root"]),
         {:ok, first_sequence} <- decimal(batch["first_sequence"]),
         {:ok, last_sequence} <- decimal(batch["last_sequence"]),
         {:ok, raw_sha256} <- hex32(batch["raw_sha256"]),
         {:ok, header} <- hex(batch["header_hex"]),
         {:ok, signature} <- hex(batch["signature_hex"]),
         activities when is_list(activities) <- batch["activities"],
         :ok <- check(sha256(raw) == raw_sha256, "raw_digest_mismatch"),
         :ok <- check(header != "" and :binary.match(raw, header) != :nomatch, "header_not_in_raw"),
         :ok <- check(signature != "" and :binary.match(raw, signature) != :nomatch, "signature_not_in_raw"),
         :ok <-
           check(
             match?(%{"cryptographic_inclusion" => "sequencer_verified"}, batch["verification"]),
             "not_sequencer_verified"
           ) do
      proof = %{
        "batch_id" => batch_id,
        "header_hex" => batch["header_hex"],
        "signature_hex" => batch["signature_hex"],
        "raw_sha256" => raw_sha256
      }

      activities
      |> Enum.reduce_while({:ok, []}, fn activity, {:ok, rows} ->
        case verify_activity(activity, {first_sequence, last_sequence}) do
          {:ok, row} ->
            {:cont,
             {:ok,
              [
                Map.merge(row, %{
                  kernel_batch_number: number,
                  kernel_batch_id: "0x" <> batch_id,
                  kernel_batch_raw_sha256: "0x" <> raw_sha256,
                  kernel_state_root: "0x" <> state_root,
                  kernel_proof: proof
                })
                | rows
              ]}}

          {:error, code} ->
            {:halt, {:error, code}}
        end
      end)
      |> case do
        {:ok, rows} -> {:ok, Enum.reverse(rows)}
        error -> error
      end
    else
      {:ok, _other_number} -> {:error, "malformed"}
      {:error, code} -> {:error, code}
      _ -> {:error, "malformed"}
    end
  end

  defp verify_activity(%{} = activity, {first_sequence, last_sequence}) do
    with {:ok, activity_id} <- hex32(activity["activity_id"]),
         {:ok, sequence} <- decimal(activity["sequence"]),
         result_code when is_integer(result_code) <- activity["result_code"],
         {:ok, receipt} <- hex(activity["receipt_hex"]),
         {:ok, canonical} <- hex(activity["canonical_hex"]),
         {:ok, receipt_sha256} <- hex32(activity["receipt_sha256"]),
         {:ok, canonical_sha256} <- hex32(activity["canonical_sha256"]),
         accounts when is_list(accounts) <- Map.get(activity, "accounts", []),
         :ok <- check(sequence >= first_sequence and sequence <= last_sequence, "sequence_out_of_range"),
         :ok <- check(sha256(receipt) == receipt_sha256, "receipt_digest_mismatch"),
         :ok <- check(sha256(canonical) == canonical_sha256, "canonical_digest_mismatch") do
      account =
        case accounts do
          [first | _] when is_binary(first) -> with {:ok, value} <- hex32(first), do: "0x" <> value
          _ -> nil
        end

      {:ok,
       %{
         origin: :kernel,
         receipt_id: "0x" <> activity_id,
         account: if(is_binary(account), do: account),
         payload_hash: "0x" <> receipt_sha256,
         status: :batch_included,
         kernel_sequence: sequence,
         kernel_activity_id: "0x" <> activity_id,
         kernel_result_code: result_code,
         kernel_receipt_sha256: "0x" <> receipt_sha256,
         kernel_canonical_sha256: "0x" <> canonical_sha256,
         kernel_verification: :sequencer_verified
       }}
    else
      {:error, code} -> {:error, code}
      _ -> {:error, "malformed"}
    end
  end

  defp verify_activity(_activity, _range), do: {:error, "malformed"}

  defp import_rows([]), do: :ok

  defp import_rows(rows) do
    case Chain.import(%{paxeer_x_receipts: %{params: rows}, timeout: :infinity}) do
      {:ok, _} ->
        :ok

      {:error, reason} ->
        Logger.error("kernel receipt import failed: #{inspect(reason)}")
        {:error, "import_failed"}

      {:error, step, reason, _changes} ->
        Logger.error("kernel receipt import failed at #{inspect(step)}: #{inspect(reason)}")
        {:error, "import_failed"}
    end
  end

  defp authorize_network(network, config) do
    with {:ok, first} <- decimal(network["first_batch"]),
         {:ok, last} <- decimal(network["last_batch"]),
         :ok <- check(network["network_id"] == config.network_id, "network_mismatch"),
         :ok <-
           check(
             is_binary(network["sequencer_public_key"]) and
               String.downcase(network["sequencer_public_key"]) == config.sequencer_public_key,
             "sequencer_key_mismatch"
           ) do
      {:ok, {first, last}}
    end
  end

  defp load_cursor(source) do
    Repo.get(KernelCursor, source) || %KernelCursor{source: source, last_batch: 0}
  end

  defp save_cursor(cursor, changes) do
    cursor
    |> KernelCursor.changeset(changes)
    |> Repo.insert_or_update!()
  end

  defp refuse(source, _cursor, code, batch) do
    save_cursor(load_cursor(source), %{refusal_code: code, refused_batch: batch})
    {:error, code, batch}
  end

  defp get_json(config, path) do
    with {:ok, body} <- get(config, path, "application/json"),
         {:ok, %{} = value} <- Jason.decode(body) do
      {:ok, value}
    else
      {:error, code} when is_binary(code) -> {:error, code}
      _ -> {:error, "malformed"}
    end
  end

  defp get_raw(config, path), do: get(config, path, "application/vnd.layerx.canonical-batch")

  defp get(config, path, accept) do
    case HTTPoison.get(config.relay_url <> path, [{"accept", accept}], @http_options) do
      {:ok, %HTTPoison.Response{status_code: 200, body: body}} -> {:ok, body}
      {:ok, %HTTPoison.Response{status_code: status}} -> {:error, "relay_status_#{status}"}
      {:error, %HTTPoison.Error{}} -> {:error, "relay_unreachable"}
    end
  end

  defp check(true, _code), do: :ok
  defp check(_condition, code), do: {:error, code}

  defp sha256(bytes), do: :sha256 |> :crypto.hash(bytes) |> Base.encode16(case: :lower)

  defp decimal(value) when is_binary(value) do
    if value =~ ~r/\A(0|[1-9][0-9]{0,18})\z/, do: {:ok, String.to_integer(value)}, else: {:error, "malformed"}
  end

  defp decimal(_value), do: {:error, "malformed"}

  defp hex32(value) when is_binary(value) do
    if value =~ ~r/\A[0-9a-f]{64}\z/, do: {:ok, value}, else: {:error, "malformed"}
  end

  defp hex32(_value), do: {:error, "malformed"}

  defp hex(value) when is_binary(value) do
    case Base.decode16(value, case: :lower) do
      {:ok, bytes} -> {:ok, bytes}
      :error -> {:error, "malformed"}
    end
  end

  defp hex(_value), do: {:error, "malformed"}

  defp env_integer!(name, default) do
    case System.get_env(name) do
      value when value in [nil, ""] and is_integer(default) ->
        default

      value when value in [nil, ""] ->
        raise ArgumentError, "#{name} must be set when INDEXER_PAXEER_X_KERNEL_RECEIPTS_RELAY_URL is set"

      value ->
        case Integer.parse(value) do
          {integer, ""} when integer >= 0 -> integer
          _ -> raise ArgumentError, "#{name} must be a non-negative integer"
        end
    end
  end

  defp env_hex!(name) do
    value = name |> System.get_env("") |> String.downcase()

    if value =~ ~r/\A[0-9a-f]+\z/ do
      value
    else
      raise ArgumentError, "#{name} must be set to the hexadecimal sequencer public key"
    end
  end
end

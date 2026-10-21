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

  alias Explorer.Chain.Import
  alias Explorer.Chain.Import.Runner.PaxeerX.Receipts, as: ReceiptsRunner
  alias Explorer.Chain.PaxeerX.Receipt.KernelCursor
  alias Explorer.Repo

  @default_interval 2_000
  @http_options [recv_timeout: 30_000, timeout: 10_000, follow_redirect: false]

  @type config :: %{
          relay_url: String.t(),
          network_id: non_neg_integer(),
          sequencer_public_key: String.t(),
          sequencer_id: String.t(),
          first_batch: pos_integer(),
          last_batch: pos_integer(),
          codec: String.t(),
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
        config = %{
          relay_url: String.trim_trailing(url, "/"),
          network_id: env_integer!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_NETWORK_ID", nil),
          sequencer_public_key: env_hex!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_SEQUENCER_PUBLIC_KEY"),
          sequencer_id: env_hex!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_SEQUENCER_ID"),
          first_batch: env_integer!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_FIRST_BATCH", nil),
          last_batch: env_integer!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_LAST_BATCH", nil),
          codec: System.fetch_env!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_CODEC"),
          interval: env_integer!("INDEXER_PAXEER_X_KERNEL_RECEIPTS_INTERVAL_MS", @default_interval)
        }

        uri = URI.parse(config.relay_url)

        unless valid_relay_url?(uri) and valid_authority?(config) and valid_codec?(config.codec) do
          raise ArgumentError, "invalid kernel receipt source, authority range or native codec"
        end

        config
    end
  end

  defp valid_relay_url?(uri) do
    uri.scheme in ["http", "https"] and is_binary(uri.host) and is_nil(uri.userinfo) and is_nil(uri.query) and
      is_nil(uri.fragment) and (uri.scheme == "https" or uri.host in ["127.0.0.1", "localhost", "::1"])
  end

  defp valid_authority?(config) do
    config.first_batch > 0 and config.last_batch >= config.first_batch and
      config.first_batch <= 9_223_372_036_854_775_807 and config.last_batch <= 18_446_744_073_709_551_615 and
      config.interval > 0 and config.network_id in 1..4_294_967_295
  end

  defp valid_codec?(codec), do: Path.type(codec) == :absolute and File.regular?(codec)

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
    cursor = load_cursor(config)

    with :ok <- check(cursor.source_identity == source_identity(config), "source_identity_mismatch"),
         {:ok, network} <- get_json(config, "/v1/sync/network"),
         {:ok, {first, last}} <- authorize_network(network, config) do
      number = max(cursor.last_batch + 1, config.first_batch)

      if number < first or number > last or number > 9_223_372_036_854_775_807 do
        refuse(config, "batch_unauthorized", number)
      else
        case get_json(config, "/v1/history/batches/#{number}") do
          {:error, "relay_status_404"} ->
            {:ok, cursor.last_batch}

          {:ok, batch} ->
            with {:ok, raw} <- get_raw(config, "/v1/sync/batches/#{number}"),
                 {:ok, rows} <- verify_batch(batch, raw, number),
                 {:ok, native} <- native_verify(config, raw),
                 :ok <- authenticated_projection(batch, native),
                 :ok <- import_rows_and_cursor(config, rows, number) do
              {:ok, number}
            else
              {:error, code} -> refuse(config, code, number)
            end

          {:error, code} ->
            refuse(config, code, number)
        end
      end
    else
      {:error, code} -> refuse(config, code, nil)
    end
  end

  defp source_identity(config) do
    [config.network_id, config.sequencer_id, config.sequencer_public_key, config.first_batch, config.last_batch]
    |> Jason.encode!()
    |> sha256()
  end

  defp authenticated_projection(batch, native) do
    fields =
      ~w(batch_number batch_id first_sequence last_sequence previous_state_root resulting_state_root network_id protocol_version epoch timestamp_ms sequencer_id header_hex signature_hex)

    check(
      Map.take(batch, fields) == Map.take(native, fields) and
        authenticated_records?(batch["activities"], native["activities"]) and
        authenticated_records?(batch["maintenance"], native["maintenance"]),
      "authenticated_projection_mismatch"
    )
  end

  defp authenticated_records?(claimed, verified) when is_list(claimed) and is_list(verified) do
    length(claimed) == length(verified) and
      Enum.all?(Enum.zip(claimed, verified), fn {left, right} ->
        is_map(left) and is_map(right) and
          Enum.all?(right, fn
            {"accounts", values} -> is_list(left["accounts"]) and Enum.sort(left["accounts"]) == Enum.sort(values)
            {key, value} -> left[key] == value
          end)
      end)
  end

  defp authenticated_records?(_, _), do: false

  defp native_verify(config, raw) do
    directory = Path.join(System.tmp_dir!(), "layerx-receipt-" <> Base.encode16(:crypto.strong_rand_bytes(16)))
    File.mkdir!(directory)
    File.chmod!(directory, 0o700)
    input = Path.join(directory, "batch")

    try do
      {:ok, file} = File.open(input, [:write, :binary, :exclusive])

      try do
        File.chmod!(input, 0o600)
        :ok = IO.binwrite(file, raw)
      after
        File.close(file)
      end

      args = [
        "verify",
        to_string(config.network_id),
        config.sequencer_id,
        config.sequencer_public_key,
        to_string(config.first_batch),
        to_string(config.last_batch),
        input
      ]

      port = Port.open({:spawn_executable, config.codec}, [:binary, :exit_status, :stderr_to_stdout, args: args])
      deadline = System.monotonic_time(:millisecond) + 30_000

      case codec_output(port, [], 0, deadline) do
        {:ok, output} ->
          case Jason.decode(output) do
            {:ok, %{} = document} -> {:ok, document}
            _ -> {:error, "native_verification_failed"}
          end

        error ->
          error
      end
    after
      File.rm(input)
      File.rmdir(directory)
    end
  rescue
    _ -> {:error, "native_verification_failed"}
  end

  defp codec_output(port, chunks, size, deadline) do
    receive do
      {^port, {:data, bytes}} when size + byte_size(bytes) <= 68_157_440 ->
        codec_output(port, [bytes | chunks], size + byte_size(bytes), deadline)

      {^port, {:data, _bytes}} ->
        Port.close(port)
        {:error, "native_verification_failed"}

      {^port, {:exit_status, 0}} ->
        {:ok, chunks |> Enum.reverse() |> IO.iodata_to_binary()}

      {^port, {:exit_status, _}} ->
        {:error, "native_verification_failed"}
    after
      max(deadline - System.monotonic_time(:millisecond), 0) ->
        Port.close(port)
        {:error, "native_verification_failed"}
    end
  end

  defp import_rows_and_cursor(config, rows, number) do
    case Repo.transaction(fn -> import_rows_and_cursor_locked(config, rows, number) end) do
      {:ok, _} -> :ok
      {:error, code} -> {:error, code}
    end
  end

  defp import_rows_and_cursor_locked(config, rows, number) do
    Repo.query!("SELECT pg_advisory_xact_lock(hashtext($1))", [config.relay_url])
    cursor = load_cursor(config)
    if cursor.source_identity != source_identity(config), do: Repo.rollback("source_identity_mismatch")
    if number != max(cursor.last_batch + 1, config.first_batch), do: Repo.rollback("cursor_changed")

    case import_rows(rows) do
      :ok -> save_cursor(cursor, %{last_batch: number, next_cursor: nil, refusal_code: nil, refused_batch: nil})
      {:error, code} -> Repo.rollback(code)
    end
  end

  defp verify_batch(batch, raw, number) do
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
         :ok <-
           check(
             sequence >= first_sequence and sequence <= last_sequence and sequence <= 9_223_372_036_854_775_807,
             "sequence_out_of_range"
           ),
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
    with {:ok, multi} <- Import.all_single_multi([ReceiptsRunner], %{paxeer_x_receipts: %{params: rows}}),
         {:ok, _} <- Repo.transaction(multi) do
      :ok
    else
      _ -> {:error, "import_failed"}
    end
  end

  defp authorize_network(network, config) do
    with {:ok, first} <- decimal(network["first_batch"]),
         {:ok, last} <- decimal(network["last_batch"]),
         :ok <- check(network["network_id"] == config.network_id, "network_mismatch"),
         :ok <- check(network["sequencer_id"] == config.sequencer_id, "sequencer_id_mismatch"),
         :ok <- check(first >= config.first_batch and last <= config.last_batch and first <= last, "batch_unauthorized"),
         :ok <-
           check(
             is_binary(network["sequencer_public_key"]) and
               String.downcase(network["sequencer_public_key"]) == config.sequencer_public_key,
             "sequencer_key_mismatch"
           ) do
      {:ok, {first, last}}
    end
  end

  defp load_cursor(config) do
    Repo.get(KernelCursor, config.relay_url) ||
      %KernelCursor{
        source: config.relay_url,
        source_identity: source_identity(config),
        last_batch: config.first_batch - 1
      }
  end

  defp save_cursor(cursor, changes) do
    cursor |> KernelCursor.changeset(changes) |> Repo.insert_or_update!()
  end

  defp refuse(config, code, batch) do
    Repo.transaction(fn ->
      Repo.query!("SELECT pg_advisory_xact_lock(hashtext($1))", [config.relay_url])
      save_cursor(load_cursor(config), %{refusal_code: code, refused_batch: batch})
    end)

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
      {:ok, %HTTPoison.Response{status_code: 200, body: body}} when byte_size(body) <= 68_157_440 -> {:ok, body}
      {:ok, %HTTPoison.Response{status_code: 200}} -> {:error, "response_too_large"}
      {:ok, %HTTPoison.Response{status_code: status}} -> {:error, "relay_status_#{status}"}
      {:error, %HTTPoison.Error{}} -> {:error, "relay_unreachable"}
    end
  end

  defp check(true, _code), do: :ok
  defp check(_condition, code), do: {:error, code}

  defp sha256(bytes), do: :sha256 |> :crypto.hash(bytes) |> Base.encode16(case: :lower)

  defp decimal(value) when is_binary(value) do
    if value =~ ~r/\A(0|[1-9][0-9]{0,19})\z/ and String.to_integer(value) <= 18_446_744_073_709_551_615,
      do: {:ok, String.to_integer(value)},
      else: {:error, "malformed"}
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

    if value =~ ~r/\A[0-9a-f]{64}\z/ do
      value
    else
      raise ArgumentError, "#{name} must be set to the hexadecimal sequencer public key"
    end
  end
end

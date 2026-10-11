defmodule Explorer.Chain.PaxeerX.SequencerReceipt do
  @moduledoc """
  Decodes, encodes and verifies sequencer receipts.

  `decode/1` and `encode/1` cover the canonical ProtocolReceipt envelope of the
  sequencer contract (section a1): protocol version 2 or 3, structure tag 0x5201,
  no program outcome, and a trailing signature flag followed by a 64-byte Ed25519
  signature. `verify/4` runs the client checks R1-R8 against the pinned
  ReceiptKeys list read from the router.
  """

  alias Explorer.Chain.Hash
  alias Explorer.Chain.PaxeerX.CometCommit
  alias Explorer.HttpClient

  @receipt_tag 0x5201
  @max_message_bytes 1_048_576
  @max_effects 512
  @min_effect_bytes 2 + 2 + 2 + 1 + 1 + (4 + 32) + 4
  @max_sequencer_effects div(@max_message_bytes, @min_effect_bytes)
  @max_effect_body 256
  @kernel_module_count 11
  @sequencer_layout_version 1
  @sequencer_protocol_version 2
  @kind_state 1
  @kind_monetary 2
  @kind_event 3
  @max_log_topics 4
  @max_fee_denom_bytes 64
  @receipt_domain "LXP/v1/receipt\0"
  @sequence_domain "PAXEER/v1/sequence\0"
  @zero32 <<0::256>>
  @uint64_max 18_446_744_073_709_551_615
  @router_timeout 10_000

  @type effect :: %{
          module_id: non_neg_integer(),
          ordinal: non_neg_integer(),
          event_type: non_neg_integer(),
          kind: 1..3,
          monetary: boolean(),
          transfer_set_root: <<_::256>>,
          body: binary()
        }

  @type t :: %{
          protocol_version: 2 | 3,
          activity_id: <<_::256>>,
          global_sequence: pos_integer(),
          previous_state_root: <<_::256>>,
          resulting_state_root: <<_::256>>,
          activity_root: <<_::256>>,
          result_status: integer(),
          effects: [effect()],
          fee_charged: non_neg_integer(),
          batch_id: <<_::256>>,
          module_id: pos_integer(),
          module_version: pos_integer(),
          parameter_version: non_neg_integer(),
          operation: non_neg_integer(),
          asset: <<_::256>>,
          amount: non_neg_integer(),
          from: <<_::256>>,
          from_balance_before: non_neg_integer(),
          from_balance_after: non_neg_integer(),
          from_sequence: non_neg_integer(),
          to: <<_::256>>,
          to_balance_before: non_neg_integer(),
          to_balance_after: non_neg_integer(),
          transfer_set_root: <<_::256>>,
          authorization_hash: <<_::256>>,
          context_hash: <<_::256>>,
          timestamp: pos_integer(),
          signature: <<_::512>> | nil
        }

  @type receipt_key :: %{
          key_id: <<_::256>>,
          public_key: <<_::256>>,
          first_height: non_neg_integer(),
          last_height: non_neg_integer()
        }

  @type decode_error ::
          :length_limit
          | :truncated
          | :version_unsupported
          | :non_canonical
          | :invalid_tag
          | :fatal_invariant
          | :unsorted_sequence

  @type step :: :r1 | :r2 | :r3 | :r4 | :r5 | :r6 | :r7 | :r8

  @doc """
  Strictly decodes canonical sequencer receipt bytes.
  """
  @spec decode(binary()) :: {:ok, t()} | {:error, decode_error()}
  def decode(bytes) when is_binary(bytes) and byte_size(bytes) > @max_message_bytes, do: {:error, :length_limit}

  def decode(<<version::16, @receipt_tag::16, protocol_version::16, rest::binary>>)
      when version in 2..3 and protocol_version == version do
    with {:ok, head, rest} <- decode_head(rest, protocol_version),
         {:ok, effects, rest} <- decode_effects(rest, head.effect_count),
         {:ok, tail, rest} <- decode_tail(rest),
         :ok <- check_effect_bound(protocol_version, head.effect_count, tail),
         {:ok, signature} <- decode_signature(rest) do
      receipt =
        head
        |> Map.delete(:effect_count)
        |> Map.merge(tail)
        |> Map.merge(%{protocol_version: protocol_version, effects: effects, signature: signature})

      if canonical_values?(receipt), do: {:ok, receipt}, else: {:error, :non_canonical}
    end
  end

  def decode(<<_version::16, _tag::16, _rest::binary>>), do: {:error, :version_unsupported}
  def decode(bytes) when is_binary(bytes), do: {:error, :truncated}

  @doc """
  Encodes a decoded receipt back into its canonical bytes.
  """
  @spec encode(t()) :: binary()
  def encode(receipt) do
    unsigned_body(receipt) <> signature_block(receipt.signature)
  end

  @doc """
  The digest the sequencer signs: sha256("LXP/v1/receipt\\0" ++ unsigned preimage).
  """
  @spec digest(t()) :: <<_::256>>
  def digest(receipt), do: :crypto.hash(:sha256, @receipt_domain <> unsigned_body(receipt) <> <<0>>)

  @doc """
  Check R2 alone: decodes the bytes, requires a byte-exact re-encoding, a signature
  and an Ed25519 signature over the receipt digest under `public_key`.
  """
  @spec verify_signature(binary(), <<_::256>>) :: {:ok, t()} | {:error, :r2}
  def verify_signature(bytes, <<_::256>> = public_key) do
    with {:ok, receipt} <- decode_signed(bytes),
         true <- signed_by?(receipt, public_key) do
      {:ok, receipt}
    else
      _ -> {:error, :r2}
    end
  end

  @doc """
  Runs R1-R6 of the client receipt checks on the `pax_sequencerReceipt` answer of
  `tx_hash` against the pinned `receipt_keys`, then R7-R8 when `opts` carries the
  committed evidence.

  Options:
    * `:prev_seq_hash` - the client's predecessor seqHash; R6 also requires the
      receipt's previous_state_root to equal it.
    * `:eth_receipt` - the `eth_getTransactionReceipt` answer; when present R7 and
      R8 run.
    * `:batch` - `%{from_seq: integer, to_seq: integer}` of the MsgSequenceBatch in
      the landing block.
    * `:chain_id`, `:commit`, `:validators` - the chain id and the `px_getCommit`
      and `px_getValidators` answers for the landing height.

  Returns `{:receipted, receipt_key}` when R1-R6 pass and no committed evidence is
  given, `{:committed, receipt_key, height}` when R1-R8 pass, otherwise
  `{:error, step}` naming the first failing step.
  """
  @spec verify(Hash.t() | binary(), map(), [receipt_key()], keyword()) ::
          {:receipted, receipt_key()} | {:committed, receipt_key(), pos_integer()} | {:error, step()}
  def verify(tx_hash, answer, receipt_keys, opts \\ []) when is_list(receipt_keys) and is_list(opts) do
    with {:ok, bytes} <- step(r1(answer), :r1),
         {:ok, receipt} <- step(decode_signed(bytes), :r2),
         {:ok, key} <- step(pinned_key(receipt_keys, receipt.context_hash), :r3),
         :ok <- step(signed_by?(receipt, key.public_key), :r2),
         {:ok, height} <- step(height_in_range(receipt.batch_id, key), :r3),
         :ok <- step(activity_matches?(receipt, tx_hash, answer), :r4),
         {:ok, view} <- step(view(receipt, height, answer), :r5),
         :ok <- step(answer_matches?(answer, view, height), :r5),
         :ok <- step(sequence_chains?(receipt, Keyword.get(opts, :prev_seq_hash)), :r6) do
      committed(receipt, view, key, opts)
    end
  end

  @doc """
  Reads the pinned ReceiptKeys list from the router `px_getReceiptKeys`.

  Returns `:disabled` when no router URL is configured; the caller then keeps the
  submitted rung.
  """
  @spec receipt_keys(String.t() | nil) :: {:ok, [receipt_key()]} | :disabled | {:error, term()}
  def receipt_keys(router_url \\ router_url())

  def receipt_keys(nil), do: :disabled

  def receipt_keys(router_url) do
    with {:ok, result} <- router_call(router_url, "px_getReceiptKeys", []) do
      decode_receipt_keys(result)
    end
  end

  @doc """
  Reads the Comet commit of `height` from the router `px_getCommit`.
  """
  @spec commit(pos_integer(), String.t() | nil) :: {:ok, map()} | :disabled | {:error, term()}
  def commit(height, router_url \\ router_url())
  def commit(_height, nil), do: :disabled

  def commit(height, router_url) when is_integer(height) and height > 0,
    do: router_object(router_url, "px_getCommit", [height])

  def commit(_height, _router_url), do: {:error, :invalid_height}

  @doc """
  Reads the validator set of `height` from the router `px_getValidators`.
  """
  @spec validators(pos_integer(), String.t() | nil) :: {:ok, map()} | :disabled | {:error, term()}
  def validators(height, router_url \\ router_url())
  def validators(_height, nil), do: :disabled

  def validators(height, router_url) when is_integer(height) and height > 0,
    do: router_object(router_url, "px_getValidators", [height])

  def validators(_height, _router_url), do: {:error, :invalid_height}

  @doc """
  Decodes the ReceiptKeys list answered by `px_getReceiptKeys`: entries of
  `{keyId, publicKey, firstHeight, lastHeight}` with 0x-hex 32-byte ids and keys
  and heights as JSON integers or 0x quantities.
  """
  @spec decode_receipt_keys(term()) :: {:ok, [receipt_key()]} | {:error, :malformed_receipt_keys}
  def decode_receipt_keys(entries) when is_list(entries) do
    entries
    |> Enum.reduce_while({:ok, []}, fn entry, {:ok, acc} ->
      case decode_receipt_key(entry) do
        {:ok, key} -> {:cont, {:ok, [key | acc]}}
        :error -> {:halt, {:error, :malformed_receipt_keys}}
      end
    end)
    |> case do
      {:ok, keys} -> {:ok, Enum.reverse(keys)}
      error -> error
    end
  end

  def decode_receipt_keys(_entries), do: {:error, :malformed_receipt_keys}

  defp decode_receipt_key(%{
         "keyId" => key_id,
         "publicKey" => public_key,
         "firstHeight" => first_height,
         "lastHeight" => last_height
       }) do
    with {:ok, <<_::256>> = key_id} <- hex_data(key_id),
         {:ok, <<_::256>> = public_key} <- hex_data(public_key),
         {:ok, first_height} <- height_value(first_height),
         {:ok, last_height} <- height_value(last_height),
         true <- first_height <= last_height do
      {:ok, %{key_id: key_id, public_key: public_key, first_height: first_height, last_height: last_height}}
    else
      _ -> :error
    end
  end

  defp decode_receipt_key(_entry), do: :error

  defp height_value(value) when is_integer(value) and value >= 0 and value <= @uint64_max, do: {:ok, value}

  defp height_value(value) do
    case quantity(value) do
      {:ok, integer} when integer <= @uint64_max -> {:ok, integer}
      _ -> :error
    end
  end

  # Decoding

  defp decode_head(bytes, protocol_version) do
    with {:ok, activity_id, rest} <- array32(bytes),
         <<global_sequence::64, rest::binary>> <- rest,
         {:ok, previous_state_root, rest} <- array32(rest),
         {:ok, resulting_state_root, rest} <- array32(rest),
         {:ok, activity_root, rest} <- array32(rest),
         <<result_status::signed-32, count::32, rest::binary>> <- rest,
         :ok <- count_within(count, if(protocol_version == 2, do: @max_sequencer_effects, else: @max_effects)),
         :ok <- if(count > div(byte_size(rest), @min_effect_bytes), do: {:error, :truncated}, else: :ok) do
      {:ok,
       %{
         activity_id: activity_id,
         global_sequence: global_sequence,
         previous_state_root: previous_state_root,
         resulting_state_root: resulting_state_root,
         activity_root: activity_root,
         result_status: result_status,
         effect_count: count
       }, rest}
    else
      {:error, _} = error -> error
      _ -> {:error, :truncated}
    end
  end

  defp count_within(count, bound) when count <= bound, do: :ok
  defp count_within(_count, _bound), do: {:error, :length_limit}

  defp decode_effects(bytes, count), do: decode_effects(bytes, count, nil, [])

  defp decode_effects(bytes, 0, _previous, acc), do: {:ok, Enum.reverse(acc), bytes}

  defp decode_effects(bytes, count, previous, acc) do
    with {:ok, effect, rest} <- decode_effect(bytes),
         :ok <- sorted_after(previous, effect) do
      decode_effects(rest, count - 1, effect, [effect | acc])
    end
  end

  defp sorted_after(nil, _effect), do: :ok

  defp sorted_after(previous, effect) do
    if {previous.module_id, previous.ordinal} < {effect.module_id, effect.ordinal},
      do: :ok,
      else: {:error, :unsorted_sequence}
  end

  defp decode_effect(<<module_id::16, ordinal::16, event_type::16, kind, monetary, rest::binary>>) do
    with :ok <- if(kind in @kind_state..@kind_event, do: :ok, else: {:error, :invalid_tag}),
         :ok <- if(monetary in 0..1, do: :ok, else: {:error, :non_canonical}),
         :ok <- if(monetary == 1 and kind != @kind_monetary, do: {:error, :fatal_invariant}, else: :ok),
         {:ok, transfer_set_root, rest} <- array32(rest),
         {:ok, body, rest} <- prefixed(rest, @max_effect_body),
         :ok <- if(module_id == 0, do: {:error, :non_canonical}, else: :ok),
         :ok <-
           if(kind == @kind_monetary and transfer_set_root == @zero32, do: {:error, :fatal_invariant}, else: :ok) do
      {:ok,
       %{
         module_id: module_id,
         ordinal: ordinal,
         event_type: event_type,
         kind: kind,
         monetary: monetary == 1,
         transfer_set_root: transfer_set_root,
         body: body
       }, rest}
    end
  end

  defp decode_effect(_bytes), do: {:error, :truncated}

  defp decode_tail(bytes) do
    with <<fee_charged::128, rest::binary>> <- bytes,
         {:ok, batch_id, rest} <- array32(rest),
         <<module_id::16, module_version::32, parameter_version::32, operation, rest::binary>> <- rest,
         {:ok, asset, rest} <- array32(rest),
         <<amount::128, rest::binary>> <- rest,
         {:ok, from, rest} <- array32(rest),
         <<from_balance_before::128, from_balance_after::128, from_sequence::64, rest::binary>> <- rest,
         {:ok, to, rest} <- array32(rest),
         <<to_balance_before::128, to_balance_after::128, rest::binary>> <- rest,
         {:ok, transfer_set_root, rest} <- array32(rest),
         {:ok, authorization_hash, rest} <- array32(rest),
         {:ok, context_hash, rest} <- array32(rest),
         <<timestamp::64, rest::binary>> <- rest do
      {:ok,
       %{
         fee_charged: fee_charged,
         batch_id: batch_id,
         module_id: module_id,
         module_version: module_version,
         parameter_version: parameter_version,
         operation: operation,
         asset: asset,
         amount: amount,
         from: from,
         from_balance_before: from_balance_before,
         from_balance_after: from_balance_after,
         from_sequence: from_sequence,
         to: to,
         to_balance_before: to_balance_before,
         to_balance_after: to_balance_after,
         transfer_set_root: transfer_set_root,
         authorization_hash: authorization_hash,
         context_hash: context_hash,
         timestamp: timestamp
       }, rest}
    else
      {:error, _} = error -> error
      _ -> {:error, :truncated}
    end
  end

  defp check_effect_bound(protocol_version, count, tail) do
    bound =
      if protocol_version == @sequencer_protocol_version and tail.module_id > @kernel_module_count and
           tail.module_version == @sequencer_layout_version,
         do: @max_sequencer_effects,
         else: @max_effects

    count_within(count, bound)
  end

  defp decode_signature(<<0>>), do: {:ok, nil}
  defp decode_signature(<<1, 64::32, signature::binary-size(64)>>), do: {:ok, signature}
  defp decode_signature(<<>>), do: {:error, :truncated}
  defp decode_signature(<<1, length::32, _::binary>>) when length > 64, do: {:error, :length_limit}
  defp decode_signature(_rest), do: {:error, :non_canonical}

  defp canonical_values?(receipt) do
    receipt.global_sequence != 0 and receipt.module_id != 0 and receipt.module_version != 0 and
      receipt.timestamp != 0 and receipt.activity_id != @zero32 and receipt.resulting_state_root != @zero32
  end

  defp array32(<<32::32, value::binary-size(32), rest::binary>>), do: {:ok, value, rest}
  defp array32(<<length::32, _::binary>>) when length > 32, do: {:error, :length_limit}
  defp array32(<<length::32, rest::binary>>) when length <= byte_size(rest), do: {:error, :non_canonical}
  defp array32(_bytes), do: {:error, :truncated}

  defp prefixed(<<length::32, _::binary>>, maximum) when length > maximum, do: {:error, :length_limit}
  defp prefixed(<<length::32, value::binary-size(length), rest::binary>>, _maximum), do: {:ok, value, rest}
  defp prefixed(_bytes, _maximum), do: {:error, :truncated}

  # Encoding

  defp unsigned_body(receipt) do
    IO.iodata_to_binary([
      <<receipt.protocol_version::16, @receipt_tag::16, receipt.protocol_version::16>>,
      arr32(receipt.activity_id),
      <<receipt.global_sequence::64>>,
      arr32(receipt.previous_state_root),
      arr32(receipt.resulting_state_root),
      arr32(receipt.activity_root),
      <<receipt.result_status::signed-32, length(receipt.effects)::32>>,
      Enum.map(receipt.effects, &encode_effect/1),
      <<receipt.fee_charged::128>>,
      arr32(receipt.batch_id),
      <<receipt.module_id::16, receipt.module_version::32, receipt.parameter_version::32, receipt.operation>>,
      arr32(receipt.asset),
      <<receipt.amount::128>>,
      arr32(receipt.from),
      <<receipt.from_balance_before::128, receipt.from_balance_after::128, receipt.from_sequence::64>>,
      arr32(receipt.to),
      <<receipt.to_balance_before::128, receipt.to_balance_after::128>>,
      arr32(receipt.transfer_set_root),
      arr32(receipt.authorization_hash),
      arr32(receipt.context_hash),
      <<receipt.timestamp::64>>
    ])
  end

  defp encode_effect(effect) do
    [
      <<effect.module_id::16, effect.ordinal::16, effect.event_type::16, effect.kind,
        if(effect.monetary, do: 1, else: 0)>>,
      arr32(effect.transfer_set_root),
      <<byte_size(effect.body)::32>>,
      effect.body
    ]
  end

  defp signature_block(nil), do: <<0>>
  defp signature_block(<<_::binary-size(64)>> = signature), do: <<1, 64::32>> <> signature

  defp arr32(<<_::binary-size(32)>> = value), do: <<32::32>> <> value

  # R1-R8

  defp step(:ok, _step), do: :ok
  defp step(true, _step), do: :ok
  defp step({:ok, value}, _step), do: {:ok, value}
  defp step(_failure, step), do: {:error, step}

  defp r1(%{"rung" => rung, "receipt" => receipt}) when rung in ["receipted", "committed"] do
    case hex_data(receipt) do
      {:ok, bytes} when bytes != <<>> -> {:ok, bytes}
      _ -> :error
    end
  end

  defp r1(_answer), do: :error

  defp decode_signed(bytes) when is_binary(bytes) do
    with {:ok, receipt} <- decode(bytes),
         true <- encode(receipt) == bytes,
         true <- receipt.signature != nil do
      {:ok, receipt}
    else
      _ -> :error
    end
  end

  defp decode_signed(_bytes), do: :error

  defp signed_by?(receipt, public_key) do
    :crypto.verify(:eddsa, :none, digest(receipt), receipt.signature, [public_key, :ed25519])
  end

  defp pinned_key(receipt_keys, context_hash) do
    case Enum.find(receipt_keys, &match?(%{key_id: ^context_hash, public_key: <<_::256>>}, &1)) do
      nil -> :error
      key -> {:ok, key}
    end
  end

  defp height_in_range(<<0::192, height::64>>, %{first_height: first, last_height: last})
       when height >= first and height <= last,
       do: {:ok, height}

  defp height_in_range(_batch_id, _key), do: :error

  defp activity_matches?(receipt, tx_hash, answer) do
    with {:ok, tx_hash} <- tx_hash_bytes(tx_hash),
         {:ok, answer_hash} <- hex_data(answer["txHash"]) do
      receipt.activity_id == tx_hash and answer_hash == tx_hash
    else
      _ -> false
    end
  end

  defp tx_hash_bytes(%Hash{bytes: <<_::256>> = bytes}), do: {:ok, bytes}
  defp tx_hash_bytes(<<_::256>> = bytes), do: {:ok, bytes}

  defp tx_hash_bytes(hex) when is_binary(hex) do
    case hex_data(hex) do
      {:ok, <<_::256>> = bytes} -> {:ok, bytes}
      _ -> :error
    end
  end

  defp tx_hash_bytes(_tx_hash), do: :error

  # The JSON view of rpc/sequencer_receipt.go sequencerReceiptView: every field is
  # derived from the receipt bytes, and the execution outputs of the answer must
  # hash to the digests the bytes carry.
  defp view(receipt, height, answer) do
    with true <- sequencer_layout?(receipt),
         {:ok, status} <- evm_status(receipt.result_status),
         [summary | log_effects] <- receipt.effects,
         true <- summary.module_id == receipt.module_id and summary.ordinal == 0 and summary.kind == @kind_state,
         {:ok, summary} <- summary(summary.body),
         true <- :crypto.hash(:sha256, summary.fee_denom) == receipt.asset,
         {:ok, from} <- addr32(receipt.from),
         {:ok, to} <- addr32(receipt.to),
         true <- summary.log_count == length(log_effects),
         {:ok, return_data} <- hex_data(answer["returnData"]),
         true <- byte_size(return_data) == summary.return_data_length,
         true <- :crypto.hash(:sha256, return_data) == receipt.activity_root,
         vm_error when is_binary(vm_error) <- answer["vmError"],
         true <- :crypto.hash(:sha256, vm_error) == summary.vm_error_digest,
         {:ok, logs} <- logs(log_effects, answer["logs"], receipt.module_id) do
      {:ok,
       %{
         "txHash" => hex(receipt.activity_id),
         "nonce" => quantity_hex(receipt.from_sequence),
         "seq" => quantity_hex(receipt.global_sequence),
         "height" => quantity_hex(height),
         "index" => quantity_hex(summary.index),
         "timestamp" => quantity_hex(receipt.timestamp),
         "status" => quantity_hex(status),
         "returnData" => hex(return_data),
         "logs" => logs,
         "gasUsed" => quantity_hex(summary.gas_used),
         "cumulativeGasUsed" => quantity_hex(summary.cumulative_gas_used),
         "effectiveGasPrice" => quantity_hex(summary.effective_gas_price),
         "feeDenom" => summary.fee_denom,
         "feeAmount" => quantity_hex(receipt.fee_charged),
         "sponsor" => hex(summary.sponsor),
         "from" => hex(from),
         "to" => hex(to),
         "contractAddress" => hex(summary.contract_address),
         "vmError" => vm_error,
         "keyId" => hex(receipt.context_hash),
         "receipt" => hex(encode(receipt))
       }}
    else
      _ -> :error
    end
  end

  defp sequencer_layout?(receipt) do
    receipt.protocol_version == @sequencer_protocol_version and
      receipt.module_version == @sequencer_layout_version and receipt.parameter_version == 0
  end

  defp evm_status(0), do: {:ok, 1}
  defp evm_status(1), do: {:ok, 0}
  defp evm_status(_result_status), do: :error

  defp summary(
         <<gas_used::64, cumulative_gas_used::64, effective_gas_price::64, index::32, contract_address::binary-size(20),
           sponsor::binary-size(20), vm_error_digest::binary-size(32), return_data_length::32, log_count::32,
           denom_length, fee_denom::binary>>
       )
       when denom_length > 0 and denom_length <= @max_fee_denom_bytes and byte_size(fee_denom) == denom_length do
    {:ok,
     %{
       gas_used: gas_used,
       cumulative_gas_used: cumulative_gas_used,
       effective_gas_price: effective_gas_price,
       index: index,
       contract_address: contract_address,
       sponsor: sponsor,
       vm_error_digest: vm_error_digest,
       return_data_length: return_data_length,
       log_count: log_count,
       fee_denom: fee_denom
     }}
  end

  defp summary(_body), do: :error

  defp addr32(<<0::96, address::binary-size(20)>>), do: {:ok, address}
  defp addr32(_word), do: :error

  defp logs(log_effects, answer_logs, module_id)
       when is_list(answer_logs) and length(answer_logs) == length(log_effects) do
    log_effects
    |> Enum.zip(answer_logs)
    |> Enum.with_index()
    |> Enum.reduce_while({:ok, []}, fn {{effect, answer_log}, index}, {:ok, acc} ->
      case log(effect, answer_log, index, module_id) do
        {:ok, log} -> {:cont, {:ok, [log | acc]}}
        :error -> {:halt, :error}
      end
    end)
    |> case do
      {:ok, logs} -> {:ok, Enum.reverse(logs)}
      :error -> :error
    end
  end

  defp logs(_log_effects, _answer_logs, _module_id), do: :error

  defp log(
         %{
           module_id: module_id,
           ordinal: ordinal,
           kind: @kind_event,
           body: <<address::binary-size(20), topic_count, rest::binary>>
         },
         %{"data" => data},
         index,
         module_id
       )
       when ordinal == index + 1 and topic_count <= @max_log_topics and byte_size(rest) == topic_count * 32 + 32 do
    topics_size = topic_count * 32
    <<topics::binary-size(topics_size), data_digest::binary-size(32)>> = rest

    with {:ok, data} <- hex_data(data),
         true <- :crypto.hash(:sha256, data) == data_digest do
      {:ok,
       %{
         "address" => hex(address),
         "topics" => for(<<topic::binary-size(32) <- topics>>, do: hex(topic)),
         "data" => hex(data),
         "logIndex" => quantity_hex(index)
       }}
    else
      _ -> :error
    end
  end

  defp log(_effect, _answer_log, _index, _module_id), do: :error

  defp answer_matches?(answer, view, height) do
    Enum.all?(view, fn
      {"logs", logs} -> logs_match?(answer["logs"], logs)
      {field, value} -> Map.fetch(answer, field) == {:ok, value}
    end) and rung_block_matches?(answer, height)
  end

  defp logs_match?(answer_logs, logs) do
    Enum.map(answer_logs, &Map.take(&1, ["address", "topics", "data", "logIndex"])) == logs
  end

  defp rung_block_matches?(%{"rung" => "receipted"} = answer, _height),
    do: not Map.has_key?(answer, "blockNumber") and not Map.has_key?(answer, "blockHash")

  defp rung_block_matches?(%{"rung" => "committed", "blockNumber" => number, "blockHash" => block_hash}, height) do
    with {:ok, number} <- quantity(number),
         {:ok, <<_::256>>} <- hex_data(block_hash) do
      number >= height
    else
      _ -> false
    end
  end

  defp rung_block_matches?(_answer, _height), do: false

  defp sequence_chains?(receipt, prev_seq_hash) do
    expected =
      :crypto.hash(
        :sha256,
        @sequence_domain <> receipt.previous_state_root <> <<receipt.global_sequence::64>> <> receipt.activity_id
      )

    expected == receipt.resulting_state_root and
      (is_nil(prev_seq_hash) or prev_seq_hash_bytes(prev_seq_hash) == {:ok, receipt.previous_state_root})
  end

  defp prev_seq_hash_bytes(<<_::256>> = bytes), do: {:ok, bytes}
  defp prev_seq_hash_bytes(hex), do: hex_data(hex)

  defp committed(receipt, view, key, opts) do
    case Keyword.fetch(opts, :eth_receipt) do
      :error ->
        {:receipted, key}

      {:ok, eth_receipt} ->
        with {:ok, height} <- step(r7(receipt, view, eth_receipt, opts[:batch]), :r7),
             {:ok, ^height} <- step(CometCommit.verify(opts[:chain_id], opts[:commit], opts[:validators]), :r8) do
          {:committed, key, height}
        else
          {:ok, _other_height} -> {:error, :r8}
          {:error, _step} = error -> error
        end
    end
  end

  defp r7(receipt, view, %{} = eth_receipt, %{from_seq: from_seq, to_seq: to_seq})
       when is_integer(from_seq) and is_integer(to_seq) do
    with {:ok, height} <- quantity(eth_receipt["blockNumber"]),
         {:ok, landing_tx_hash} <- hex_data(eth_receipt["transactionHash"]),
         {:ok, status} <- quantity(eth_receipt["status"]),
         {:ok, gas_used} <- quantity(eth_receipt["gasUsed"]),
         {:ok, logs} <- eth_logs(eth_receipt["logs"]),
         {:ok, receipt_height} <- quantity(view["height"]),
         true <- height >= receipt_height,
         true <- landing_tx_hash == receipt.activity_id,
         true <- quantity_hex(status) == view["status"] and quantity_hex(gas_used) == view["gasUsed"],
         true <- logs == Enum.map(view["logs"], &Map.take(&1, ["address", "topics", "data"])),
         true <- from_seq <= receipt.global_sequence and receipt.global_sequence <= to_seq do
      {:ok, height}
    else
      _ -> :error
    end
  end

  defp r7(_receipt, _view, _eth_receipt, _batch), do: :error

  defp eth_logs(logs) when is_list(logs) do
    logs
    |> Enum.reduce_while({:ok, []}, fn
      %{"address" => address, "topics" => topics, "data" => data}, {:ok, acc} when is_list(topics) ->
        with {:ok, <<_::160>> = address} <- hex_data(address),
             {:ok, topics} <- hex_list(topics),
             {:ok, data} <- hex_data(data) do
          {:cont,
           {:ok, [%{"address" => hex(address), "topics" => Enum.map(topics, &hex/1), "data" => hex(data)} | acc]}}
        else
          _ -> {:halt, :error}
        end

      _log, _acc ->
        {:halt, :error}
    end)
    |> case do
      {:ok, logs} -> {:ok, Enum.reverse(logs)}
      :error -> :error
    end
  end

  defp eth_logs(_logs), do: :error

  defp hex_list(values) do
    values
    |> Enum.reduce_while({:ok, []}, fn value, {:ok, acc} ->
      case hex_data(value) do
        {:ok, <<_::256>> = topic} -> {:cont, {:ok, [topic | acc]}}
        _ -> {:halt, :error}
      end
    end)
    |> case do
      {:ok, topics} -> {:ok, Enum.reverse(topics)}
      :error -> :error
    end
  end

  # Router

  defp router_url do
    Application.get_env(:explorer, __MODULE__, [])[:router_url]
  end

  defp router_object(router_url, method, params) do
    case router_call(router_url, method, params) do
      {:ok, %{} = result} -> {:ok, result}
      {:ok, other} -> {:error, {:unexpected_result, other}}
      error -> error
    end
  end

  defp router_call(router_url, method, params) do
    body = Jason.encode!(%{jsonrpc: "2.0", id: 1, method: method, params: params})

    case HttpClient.post(router_url, body, [{"Content-Type", "application/json"}],
           recv_timeout: @router_timeout,
           timeout: @router_timeout
         ) do
      {:ok, %{status_code: 200, body: response}} -> router_result(response)
      {:ok, %{status_code: status}} -> {:error, {:http_status, status}}
      {:error, reason} -> {:error, reason}
    end
  end

  defp router_result(response) do
    case Jason.decode(response) do
      {:ok, %{"result" => nil}} -> {:error, :not_found}
      {:ok, %{"result" => result}} -> {:ok, result}
      {:ok, %{"error" => error}} -> {:error, {:rpc_error, error}}
      _ -> {:error, :malformed_response}
    end
  end

  # Hex

  defp hex(bytes), do: "0x" <> Base.encode16(bytes, case: :lower)

  defp quantity_hex(integer), do: "0x" <> String.downcase(Integer.to_string(integer, 16))

  defp hex_data("0x" <> digits) when rem(byte_size(digits), 2) == 0, do: Base.decode16(digits, case: :mixed)
  defp hex_data(_value), do: :error

  defp quantity("0x" <> digits) when digits != "" do
    case Integer.parse(digits, 16) do
      {integer, ""} when integer >= 0 -> {:ok, integer}
      _ -> :error
    end
  end

  defp quantity(_value), do: :error
end

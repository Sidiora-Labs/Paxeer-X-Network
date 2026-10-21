defmodule Explorer.Chain.PaxeerX.CometCommit do
  @moduledoc """
  Verifies a Comet commit against the validator set of its height.

  `verify/3` takes the decoded answers of the router `px_getCommit(height)` and
  `px_getValidators(height)` calls. It recomputes the header hash and the validator
  set hash the way the consensus engine does (RFC 6962 merkle roots over the
  protobuf encodings of `Header.Hash` and `ValidatorSet.Hash`), checks every
  precommit signature against its canonical vote sign bytes, and accepts the
  commit only when the validators whose signatures verify hold more than two
  thirds of the total voting power.
  """

  import Bitwise

  @precommit_type 2
  @flag_absent 1
  @flag_commit 2
  @flag_nil 3
  @ed25519_type "tendermint/PubKeyEd25519"
  @ed25519_key_bytes 32
  @ed25519_signature_bytes 64
  @address_bytes 20
  @max_validators 10_000
  @int64_max 9_223_372_036_854_775_807
  @uint32_max 4_294_967_295
  @uint64_max 18_446_744_073_709_551_615
  @int32_max 2_147_483_647
  @unix_epoch ~N[1970-01-01 00:00:00]
  @timestamp_pattern ~r/\A(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z\z/

  @type reason ::
          :malformed_header
          | :malformed_commit
          | :malformed_validators
          | :chain_id_mismatch
          | :height_mismatch
          | :header_hash_mismatch
          | :incomplete_validator_set
          | :duplicate_validator
          | :validator_address_mismatch
          | :validators_hash_mismatch
          | :signature_count_mismatch
          | :insufficient_voting_power
          | {:malformed_signature, non_neg_integer()}
          | {:signer_mismatch, non_neg_integer()}
          | {:invalid_signature, non_neg_integer()}

  @doc """
  Verifies the commit of `signed_header` (the `px_getCommit` answer or its
  `signed_header` member) under `chain_id` with the `px_getValidators` answer of
  the same height.

  Returns `{:ok, height}` when more than two thirds of the voting power signed the
  header, otherwise `{:error, reason}`.
  """
  @spec verify(String.t(), map(), map()) :: {:ok, pos_integer()} | {:error, reason()}
  def verify(chain_id, %{"signed_header" => signed_header}, validators), do: verify(chain_id, signed_header, validators)

  def verify(chain_id, %{"header" => header, "commit" => commit}, validators)
      when is_binary(chain_id) and chain_id != "" and is_map(validators) do
    with {:ok, header} <- decode_header(header),
         :ok <- expect(header.chain_id == chain_id, :chain_id_mismatch),
         {:ok, commit} <- decode_commit(commit),
         :ok <- expect(commit.height == header.height, :height_mismatch),
         :ok <- expect(header_hash(header) == commit.block_id.hash, :header_hash_mismatch),
         {:ok, validators} <- decode_validators(validators, header.height),
         :ok <- expect(validator_set_hash(validators) == header.validators_hash, :validators_hash_mismatch),
         :ok <- expect(length(commit.signatures) == length(validators), :signature_count_mismatch),
         {:ok, signed_power} <- tally(chain_id, commit, validators) do
      total_power = Enum.reduce(validators, 0, &(&1.power + &2))
      if signed_power * 3 > total_power * 2, do: {:ok, header.height}, else: {:error, :insufficient_voting_power}
    end
  end

  def verify(_chain_id, _signed_header, _validators), do: {:error, :malformed_commit}

  defp tally(chain_id, commit, validators) do
    commit.signatures
    |> Enum.zip(validators)
    |> Enum.with_index()
    |> Enum.reduce_while({:ok, 0}, fn {{signature, validator}, index}, {:ok, power} ->
      case check_signature(chain_id, commit, signature, validator, index) do
        :counted -> {:cont, {:ok, power + validator.power}}
        :ignored -> {:cont, {:ok, power}}
        {:error, _} = error -> {:halt, error}
      end
    end)
  end

  defp check_signature(_chain_id, _commit, %{"block_id_flag" => flag}, _validator, _index)
       when flag in [@flag_absent, @flag_nil],
       do: :ignored

  defp check_signature(
         chain_id,
         commit,
         %{
           "block_id_flag" => @flag_commit,
           "validator_address" => address,
           "timestamp" => timestamp,
           "signature" => signature
         },
         validator,
         index
       ) do
    with {:ok, address} <- hex(address, @address_bytes),
         {:ok, timestamp} <- timestamp(timestamp),
         {:ok, signature} <- base64(signature, @ed25519_signature_bytes) do
      cond do
        address != validator.address ->
          {:error, {:signer_mismatch, index}}

        :crypto.verify(:eddsa, :none, sign_bytes(chain_id, commit, timestamp), signature, [
          validator.pub_key,
          :ed25519
        ]) ->
          :counted

        true ->
          {:error, {:invalid_signature, index}}
      end
    else
      _ -> {:error, {:malformed_signature, index}}
    end
  end

  defp check_signature(_chain_id, _commit, _signature, _validator, index), do: {:error, {:malformed_signature, index}}

  defp sign_bytes(chain_id, commit, timestamp) do
    canonical_block_id =
      bytes_field(1, commit.block_id.hash) <> message_field(2, part_set_header(commit.block_id.parts))

    canonical_vote =
      varint_field(1, @precommit_type) <>
        sfixed64_field(2, commit.height) <>
        sfixed64_field(3, commit.round) <>
        message_field(4, canonical_block_id) <>
        message_field(5, timestamp_proto(timestamp)) <>
        bytes_field(6, chain_id)

    varint(byte_size(canonical_vote)) <> canonical_vote
  end

  defp header_hash(header) do
    merkle_root([
      varint_field(1, header.version_block) <> varint_field(2, header.version_app),
      bytes_field(1, header.chain_id),
      varint_field(1, header.height),
      timestamp_proto(header.time),
      block_id_proto(header.last_block_id),
      bytes_field(1, header.last_commit_hash),
      bytes_field(1, header.data_hash),
      bytes_field(1, header.validators_hash),
      bytes_field(1, header.next_validators_hash),
      bytes_field(1, header.consensus_hash),
      bytes_field(1, header.app_hash),
      bytes_field(1, header.last_results_hash),
      bytes_field(1, header.evidence_hash),
      bytes_field(1, header.proposer_address)
    ])
  end

  defp validator_set_hash(validators) do
    validators
    |> Enum.map(&(message_field(1, message_field(1, &1.pub_key)) <> varint_field(2, &1.power)))
    |> merkle_root()
  end

  defp block_id_proto(block_id), do: bytes_field(1, block_id.hash) <> message_field(2, part_set_header(block_id.parts))

  defp part_set_header(parts), do: varint_field(1, parts.total) <> bytes_field(2, parts.hash)

  defp timestamp_proto({seconds, nanos}), do: varint_field(1, seconds) <> varint_field(2, nanos)

  defp merkle_root([]), do: :crypto.hash(:sha256, <<>>)
  defp merkle_root([leaf]), do: :crypto.hash(:sha256, <<0, leaf::binary>>)

  defp merkle_root(items) do
    {left, right} = Enum.split(items, split_point(length(items)))
    :crypto.hash(:sha256, <<1>> <> merkle_root(left) <> merkle_root(right))
  end

  defp split_point(count) do
    largest = 1 <<< (length(Integer.digits(count, 2)) - 1)
    if largest == count, do: largest >>> 1, else: largest
  end

  defp varint(value) when value < 0, do: varint(value + (1 <<< 64))
  defp varint(value) when value < 0x80, do: <<value>>
  defp varint(value), do: <<(value &&& 0x7F) ||| 0x80>> <> varint(value >>> 7)

  defp field_key(field, wire_type), do: varint(field <<< 3 ||| wire_type)

  defp varint_field(_field, 0), do: <<>>
  defp varint_field(field, value), do: field_key(field, 0) <> varint(value)

  defp sfixed64_field(_field, 0), do: <<>>
  defp sfixed64_field(field, value), do: field_key(field, 1) <> <<value::little-signed-64>>

  defp bytes_field(_field, <<>>), do: <<>>
  defp bytes_field(field, value), do: message_field(field, value)

  defp message_field(field, value), do: field_key(field, 2) <> varint(byte_size(value)) <> value

  defp decode_header(%{
         "version" => %{"block" => block, "app" => app},
         "chain_id" => chain_id,
         "height" => height,
         "time" => time,
         "last_block_id" => last_block_id,
         "last_commit_hash" => last_commit_hash,
         "data_hash" => data_hash,
         "validators_hash" => validators_hash,
         "next_validators_hash" => next_validators_hash,
         "consensus_hash" => consensus_hash,
         "app_hash" => app_hash,
         "last_results_hash" => last_results_hash,
         "evidence_hash" => evidence_hash,
         "proposer_address" => proposer_address
       })
       when is_binary(chain_id) do
    with {:ok, version_block} <- decimal(block, 0, @uint64_max),
         {:ok, version_app} <- decimal(app, 0, @uint64_max),
         {:ok, height} <- decimal(height, 1, @int64_max),
         {:ok, time} <- timestamp(time),
         {:ok, last_block_id} <- block_id(last_block_id),
         {:ok, last_commit_hash} <- hex(last_commit_hash),
         {:ok, data_hash} <- hex(data_hash),
         {:ok, validators_hash} <- hex(validators_hash),
         {:ok, next_validators_hash} <- hex(next_validators_hash),
         {:ok, consensus_hash} <- hex(consensus_hash),
         {:ok, app_hash} <- hex(app_hash),
         {:ok, last_results_hash} <- hex(last_results_hash),
         {:ok, evidence_hash} <- hex(evidence_hash),
         {:ok, proposer_address} <- hex(proposer_address) do
      {:ok,
       %{
         version_block: version_block,
         version_app: version_app,
         chain_id: chain_id,
         height: height,
         time: time,
         last_block_id: last_block_id,
         last_commit_hash: last_commit_hash,
         data_hash: data_hash,
         validators_hash: validators_hash,
         next_validators_hash: next_validators_hash,
         consensus_hash: consensus_hash,
         app_hash: app_hash,
         last_results_hash: last_results_hash,
         evidence_hash: evidence_hash,
         proposer_address: proposer_address
       }}
    else
      _ -> {:error, :malformed_header}
    end
  end

  defp decode_header(_header), do: {:error, :malformed_header}

  defp decode_commit(%{"height" => height, "round" => round, "block_id" => block_id, "signatures" => signatures})
       when is_integer(round) and round >= 0 and round <= @int32_max and is_list(signatures) and signatures != [] and
              length(signatures) <= @max_validators do
    with {:ok, height} <- decimal(height, 1, @int64_max),
         {:ok, %{hash: <<_::binary-size(32)>>, parts: %{hash: <<_::binary-size(32)>>}} = block_id} <-
           block_id(block_id) do
      {:ok, %{height: height, round: round, block_id: block_id, signatures: signatures}}
    else
      _ -> {:error, :malformed_commit}
    end
  end

  defp decode_commit(_commit), do: {:error, :malformed_commit}

  defp block_id(%{"hash" => hash, "parts" => %{"total" => total, "hash" => parts_hash}})
       when is_integer(total) and total >= 0 and total <= @uint32_max do
    with {:ok, hash} <- hex(hash),
         {:ok, parts_hash} <- hex(parts_hash) do
      {:ok, %{hash: hash, parts: %{total: total, hash: parts_hash}}}
    end
  end

  defp block_id(_block_id), do: :error

  defp decode_validators(
         %{"block_height" => block_height, "validators" => validators, "count" => count, "total" => total},
         height
       )
       when is_list(validators) and validators != [] and length(validators) <= @max_validators do
    size = Integer.to_string(length(validators))

    with {:ok, ^height} <- decimal(block_height, 1, @int64_max),
         true <- count == size and total == size,
         {:ok, decoded} <- decode_validator_list(validators),
         :ok <- expect(unique?(decoded), :duplicate_validator) do
      {:ok, decoded}
    else
      {:ok, _other_height} -> {:error, :height_mismatch}
      false -> {:error, :incomplete_validator_set}
      {:error, _} = error -> error
      _ -> {:error, :malformed_validators}
    end
  end

  defp decode_validators(_validators, _height), do: {:error, :malformed_validators}

  defp decode_validator_list(validators) do
    validators
    |> Enum.reduce_while({:ok, []}, fn validator, {:ok, acc} ->
      case decode_validator(validator) do
        {:ok, decoded} -> {:cont, {:ok, [decoded | acc]}}
        {:error, _} = error -> {:halt, error}
      end
    end)
    |> case do
      {:ok, decoded} -> {:ok, Enum.reverse(decoded)}
      error -> error
    end
  end

  defp decode_validator(%{
         "address" => address,
         "pub_key" => %{"type" => @ed25519_type, "value" => pub_key},
         "voting_power" => power
       }) do
    with {:ok, address} <- hex(address, @address_bytes),
         {:ok, pub_key} <- base64(pub_key, @ed25519_key_bytes),
         {:ok, power} <- decimal(power, 1, @int64_max) do
      <<derived::binary-size(@address_bytes), _::binary>> = :crypto.hash(:sha256, pub_key)

      if derived == address,
        do: {:ok, %{address: address, pub_key: pub_key, power: power}},
        else: {:error, :validator_address_mismatch}
    else
      _ -> {:error, :malformed_validators}
    end
  end

  defp decode_validator(_validator), do: {:error, :malformed_validators}

  defp unique?(validators), do: validators |> Enum.uniq_by(& &1.address) |> length() == length(validators)

  defp decimal(value, minimum, maximum) when is_binary(value) do
    case Integer.parse(value) do
      {integer, ""} when integer >= minimum and integer <= maximum -> {:ok, integer}
      _ -> :error
    end
  end

  defp decimal(_value, _minimum, _maximum), do: :error

  defp hex(value) when is_binary(value), do: Base.decode16(value, case: :mixed)
  defp hex(_value), do: :error

  defp hex(value, size) do
    case hex(value) do
      {:ok, <<_::binary-size(size)>> = bytes} -> {:ok, bytes}
      _ -> :error
    end
  end

  defp base64(value, size) when is_binary(value) do
    case Base.decode64(value) do
      {:ok, <<_::binary-size(size)>> = bytes} -> {:ok, bytes}
      _ -> :error
    end
  end

  defp base64(_value, _size), do: :error

  defp timestamp(value) when is_binary(value) do
    with [_, base | fraction] <- Regex.run(@timestamp_pattern, value),
         {:ok, naive} <- NaiveDateTime.from_iso8601(base) do
      nanos =
        case fraction do
          [digits] -> digits |> String.pad_trailing(9, "0") |> String.to_integer()
          [] -> 0
        end

      {:ok, {NaiveDateTime.diff(naive, @unix_epoch), nanos}}
    else
      _ -> :error
    end
  end

  defp timestamp(_value), do: :error

  defp expect(true, _reason), do: :ok
  defp expect(false, reason), do: {:error, reason}
end

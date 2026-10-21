defmodule Explorer.Chain.PaxeerX.AccountBindingTest do
  use Explorer.DataCase

  import ExUnit.CaptureLog

  alias ABI.TypeEncoder
  alias Ecto.Multi
  alias Explorer.Chain.{Address, Hash, Import}
  alias Explorer.Chain.Import.Runner.PaxeerX.AccountBindings
  alias Explorer.Chain.PaxeerX.{AccountBinding, Identity, UnifiedAccount}
  alias Indexer.Transform.PaxeerXLogs

  @addr_precompile "0x0000000000000000000000000000000000001004"
  @evm_address "0x7be8076f4ea4a4ad08075c2508e481d6c946d12b"
  @pax_address "pax1005qwm6w5jj26zq8tsjs3eyp6my5d5fthrlqk3"

  setup do
    chain_type = Application.get_env(:explorer, :chain_type)
    Application.put_env(:explorer, :chain_type, :paxeer_x)
    on_exit(fn -> Application.put_env(:explorer, :chain_type, chain_type) end)
    :ok
  end

  describe "changeset/2" do
    test "accepts the four identity spellings of a bound account" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = attributes(block, transaction)
      changeset = AccountBinding.changeset(%AccountBinding{}, attrs)

      assert changeset.valid?
      assert {:ok, binding} = Repo.insert(changeset)
      assert binding.bound
      assert binding.pax_address == "pax1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
      assert binding.evm_address_hash == attrs.evm_address_hash
      assert binding.layerx_did == attrs.layerx_did
      assert binding.layerx_account == attrs.layerx_account
      assert binding.nonce == 7
      assert binding.block_number == block.number
      assert binding.transaction_hash == transaction.hash
    end

    test "requires the key, the address and the bound flag" do
      changeset = AccountBinding.changeset(%AccountBinding{}, %{})

      refute changeset.valid?

      errors = changeset_errors(changeset)

      assert errors[:transaction_hash] == ["can't be blank"]
      assert errors[:log_index] == ["can't be blank"]
      assert errors[:block_hash] == ["can't be blank"]
      assert errors[:block_number] == ["can't be blank"]
      assert errors[:evm_address_hash] == ["can't be blank"]
      assert errors[:bound] == ["can't be blank"]
    end

    test "accepts an unbinding that carries no kernel identity yet" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs =
        block
        |> attributes(transaction)
        |> Map.merge(%{bound: false, pax_address: nil, layerx_did: nil, layerx_account: nil})

      assert {:ok, binding} = %AccountBinding{} |> AccountBinding.changeset(attrs) |> Repo.insert()
      refute binding.bound
      assert is_nil(binding.layerx_did)
    end

    test "rejects a binding whose transaction is not indexed" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = block |> attributes(transaction) |> Map.put(:transaction_hash, transaction_hash())

      assert {:error, changeset} = %AccountBinding{} |> AccountBinding.changeset(attrs) |> Repo.insert()
      assert changeset_errors(changeset)[:transaction_hash] == ["does not exist"]
    end

    test "refuses a complete binding when only the bound flag is missing" do
      %{block: block, transaction: transaction} = block_with_transaction()
      attrs = block |> attributes(transaction) |> Map.delete(:bound)

      assert {:error, [changeset]} = Import.all(%{paxeer_x_account_bindings: %{params: [attrs]}})
      assert changeset_errors(changeset) == %{bound: ["can't be blank"]}
      assert Repo.aggregate(AccountBinding, :count) == 0
    end
  end

  describe "native binding logs" do
    test "keeps both binding topics at addr with their exact signatures" do
      definitions = Enum.filter(PaxeerXLogs.definitions(), &(&1.table == :lx_account_bindings))
      assert Enum.sort(Enum.map(definitions, & &1.name)) == ["LayerXBound", "LayerXUnbound"]

      for definition <- definitions do
        assert definition.address == @addr_precompile
        assert definition.signature == definition.name <> "(address,bytes32,uint64)"
        assert definition.topic == hex(ExKeccak.hash_256(definition.signature))

        assert definition.arguments == [
                 {"evm", :address, true},
                 {"didPublicKey", {:bytes, 32}, true},
                 {"nonce", {:uint, 64}, false}
               ]

        assert definition.bound == (definition.name == "LayerXBound")
      end
    end

    test "decodes LayerXBound into a persistent binding that resolves its DID" do
      %{block: block, transaction: transaction} = block_with_transaction()
      log = binding_log(block, transaction, 3, "LayerXBound", 7)
      did = "did:layerx:" <> Base.encode16(<<0x22::256>>, case: :lower)
      {:ok, address_hash} = Hash.Address.cast(@evm_address)

      assert %{lx_account_bindings: [row], lx_market_events: [], undecoded_log_count: 0} = PaxeerXLogs.parse([log])

      assert row == %{
               transaction_hash: transaction.hash,
               block_hash: block.hash,
               block_number: block.number,
               log_index: 3,
               event_name: "LayerXBound",
               bound: true,
               evm_address_hash: @evm_address,
               layerx_did: did,
               nonce: 7,
               parameters: %{"evm" => @evm_address, "didPublicKey" => hex(<<0x22::256>>), "nonce" => 7}
             }

      options = %{paxeer_x_account_bindings: %{params: [row]}}
      assert {:ok, %{insert_paxeer_x_account_bindings: [_]}} = Import.all(options)
      assert {:ok, %{insert_paxeer_x_account_bindings: []}} = Import.all(options)
      assert [stored] = Repo.all(AccountBinding.only_consensus_query())
      assert stored.transaction_hash == transaction.hash
      assert stored.block_hash == block.hash
      assert stored.block_number == block.number
      assert stored.log_index == 3
      assert stored.evm_address_hash == address_hash
      assert stored.event_name == "LayerXBound"
      assert stored.parameters == row.parameters
      assert stored.bound == true
      assert stored.layerx_did == did
      assert stored.nonce == 7
      assert stored.block_consensus == true
      assert is_nil(stored.pax_address)
      assert is_nil(stored.layerx_account)

      assert UnifiedAccount.identities(address_hash) == %{
               evm: Address.checksum(address_hash),
               pax: nil,
               did: did,
               kernel_account: nil,
               bound: true
             }

      assert {:ok, identity} = Identity.parse(did)
      assert {:ok, ^address_hash} = UnifiedAccount.resolve(identity, [])
    end

    test "the later LayerXUnbound log removes the current identity view" do
      %{block: block, transaction: transaction} = block_with_transaction()

      logs = [
        binding_log(block, transaction, 3, "LayerXBound", 7),
        binding_log(block, transaction, 4, "LayerXUnbound", 8)
      ]

      assert %{lx_account_bindings: [bound, unbound], undecoded_log_count: 0} = PaxeerXLogs.parse(logs)
      assert bound.bound == true
      assert unbound.bound == false
      assert unbound.layerx_did == bound.layerx_did
      assert unbound.event_name == "LayerXUnbound"
      assert unbound.nonce == 8

      assert {:ok, %{insert_paxeer_x_account_bindings: inserted}} =
               Import.all(%{paxeer_x_account_bindings: %{params: [unbound, bound]}})

      assert length(inserted) == 2
      {:ok, address_hash} = Hash.Address.cast(@evm_address)

      assert UnifiedAccount.identities(address_hash) == %{
               evm: Address.checksum(address_hash),
               pax: nil,
               did: nil,
               kernel_account: nil,
               bound: false
             }

      assert {:ok, identity} = Identity.parse(bound.layerx_did)
      assert :error = UnifiedAccount.resolve(identity, [])
    end

    test "refuses a binding topic from another emitter and missing binding arguments" do
      %{block: block, transaction: transaction} = block_with_transaction()
      log = binding_log(block, transaction, 3, "LayerXBound", 7)

      assert %{lx_account_bindings: [], undecoded_log_count: 0} =
               PaxeerXLogs.parse([Map.put(log, :address_hash, @evm_address)])

      invalid_logs = [
        {Map.put(log, :address_hash, "0x0000000000000000000000000000000000001015"), "No definition for topic0"},
        {Map.put(log, :third_topic, nil), "missing an indexed topic"},
        {Map.put(log, :data, "0x"), "carries no data for its unindexed arguments"}
      ]

      for {invalid_log, message} <- invalid_logs do
        assert capture_log(fn ->
                 assert %{lx_account_bindings: [], undecoded_log_count: 1} = PaxeerXLogs.parse([invalid_log])
               end) =~ message
      end

      assert Repo.aggregate(AccountBinding, :count) == 0
    end
  end

  describe "account identity reads" do
    test "resolves all four spellings and a prefixless EVM address to the stored binding" do
      %{block: block, transaction: transaction} = block_with_transaction()
      {:ok, address_hash} = Hash.Address.cast(@evm_address)

      attrs =
        block |> attributes(transaction) |> Map.merge(%{evm_address_hash: address_hash, pax_address: @pax_address})

      assert {:ok, %{insert_paxeer_x_account_bindings: [_]}} = run_changes([attrs])

      assert UnifiedAccount.identities(address_hash) == %{
               evm: Address.checksum(address_hash),
               pax: @pax_address,
               did: attrs.layerx_did,
               kernel_account: attrs.layerx_account,
               bound: true
             }

      spellings = [
        @evm_address,
        String.trim_leading(@evm_address, "0x"),
        @pax_address,
        attrs.layerx_did,
        attrs.layerx_account
      ]

      for spelling <- spellings do
        assert {:ok, identity} = Identity.parse(spelling)
        assert {:ok, ^address_hash} = UnifiedAccount.resolve(identity, [])
      end
    end

    test "rejects malformed EVM spellings and returns the unbound view without a row" do
      {:ok, address_hash} = Hash.Address.cast(@evm_address)
      assert {:ok, %Identity{kind: :evm, evm: ^address_hash}} = Identity.parse(@evm_address)
      assert {:ok, %Identity{kind: :evm, evm: ^address_hash}} = Identity.parse(String.trim_leading(@evm_address, "0x"))

      for spelling <- ["0xaa", "aa", @evm_address <> "00", "0x" <> String.duplicate("g", 40), nil] do
        assert :error = Identity.parse(spelling)
      end

      assert UnifiedAccount.identities(address_hash) == %{
               evm: Address.checksum(address_hash),
               pax: nil,
               did: nil,
               kernel_account: nil,
               bound: false
             }
    end
  end

  describe "run/3" do
    test "inserts a binding row keyed by transaction hash and log index" do
      %{block: block, transaction: transaction} = block_with_transaction()

      assert {:ok, %{insert_paxeer_x_account_bindings: [inserted]}} =
               run_changes([attributes(block, transaction)])

      assert inserted.transaction_hash == transaction.hash
      assert inserted.log_index == 3
      assert inserted.block_consensus

      assert [stored] = Repo.all(AccountBinding)
      assert stored.transaction_hash == transaction.hash
      assert stored.layerx_account == inserted.layerx_account
    end

    test "leaves the stored row untouched when the same log is imported again" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = attributes(block, transaction)

      assert {:ok, %{insert_paxeer_x_account_bindings: [_]}} = run_changes([attrs])

      assert {:ok, %{insert_paxeer_x_account_bindings: []}} =
               run_changes([Map.merge(attrs, %{bound: false, layerx_did: nil})])

      assert [stored] = Repo.all(AccountBinding)
      assert stored.bound
      assert stored.layerx_did == attrs.layerx_did
    end

    test "keeps the rows of two logs of the same transaction apart" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = attributes(block, transaction)

      assert {:ok, %{insert_paxeer_x_account_bindings: inserted}} =
               run_changes([attrs, Map.merge(attrs, %{log_index: 4, bound: false})])

      assert length(inserted) == 2
      assert Repo.aggregate(AccountBinding, :count) == 2
    end

    test "handles an empty changes list" do
      assert {:ok, %{insert_paxeer_x_account_bindings: []}} = run_changes([])
    end
  end

  describe "only_consensus_query/0" do
    test "drops the rows of a block that lost consensus" do
      %{block: kept_block, transaction: kept_transaction} = block_with_transaction()
      %{block: reorged_block, transaction: reorged_transaction} = block_with_transaction()
      kept_attrs = attributes(kept_block, kept_transaction)
      reorged_attrs = attributes(reorged_block, reorged_transaction)

      assert {:ok, _} =
               run_changes([
                 kept_attrs,
                 reorged_attrs
               ])

      Repo.update!(Ecto.Changeset.change(reorged_block, consensus: false))

      assert [kept] = Repo.all(AccountBinding.only_consensus_query())
      assert kept.transaction_hash == kept_transaction.hash

      assert UnifiedAccount.identities(kept.evm_address_hash).did == kept.layerx_did

      assert UnifiedAccount.identities(reorged_attrs.evm_address_hash) == %{
               evm: Address.checksum(reorged_attrs.evm_address_hash),
               pax: nil,
               did: nil,
               kernel_account: nil,
               bound: false
             }

      {1, nil} =
        [reorged_block.hash]
        |> AccountBinding.lose_consensus_query()
        |> Repo.update_all(set: [block_consensus: false])

      assert [false] =
               AccountBinding
               |> where([binding], binding.block_hash == ^reorged_block.hash)
               |> select([binding], binding.block_consensus)
               |> Repo.all()
    end
  end

  defp block_with_transaction do
    block = insert(:block)
    transaction = :transaction |> insert() |> with_block(block)

    %{block: block, transaction: transaction}
  end

  defp attributes(block, transaction) do
    did_key = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

    %{
      transaction_hash: transaction.hash,
      log_index: 3,
      block_hash: block.hash,
      block_number: block.number,
      evm_address_hash: address_hash(),
      pax_address: "pax1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq",
      layerx_did: "did:layerx:" <> did_key,
      layerx_account: "agent:did:layerx:" <> did_key <> ":main",
      bound: true,
      nonce: 7
    }
  end

  defp run_changes(changes) when is_list(changes) do
    Multi.new()
    |> AccountBindings.run(changes, %{
      timeout: :infinity,
      timestamps: %{inserted_at: DateTime.utc_now(), updated_at: DateTime.utc_now()}
    })
    |> Repo.transaction()
  end

  defp binding_log(block, transaction, index, event_name, nonce) do
    "0x" <> address = @evm_address
    evm = Base.decode16!(address, case: :mixed)

    %{
      address_hash: @addr_precompile,
      block_hash: block.hash,
      block_number: block.number,
      transaction_hash: transaction.hash,
      index: index,
      first_topic: hex(ExKeccak.hash_256(event_name <> "(address,bytes32,uint64)")),
      second_topic: hex(TypeEncoder.encode([evm], [:address])),
      third_topic: hex(TypeEncoder.encode([<<0x22::256>>], [{:bytes, 32}])),
      fourth_topic: nil,
      data: hex(TypeEncoder.encode([nonce], [{:uint, 64}]))
    }
  end

  defp hex(bytes), do: "0x" <> Base.encode16(bytes, case: :lower)
end

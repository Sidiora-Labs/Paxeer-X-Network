defmodule Explorer.Chain.PaxeerX.MarketEventTest do
  use Explorer.DataCase

  import ExUnit.CaptureLog

  alias ABI.TypeEncoder
  alias Ecto.Multi
  alias Explorer.Chain.Import
  alias Explorer.Chain.Import.Runner.PaxeerX.MarketEvents
  alias Explorer.Chain.PaxeerX.MarketEvent
  alias Indexer.Transform.PaxeerXLogs

  @precompiles %{
    exchange: "0x0000000000000000000000000000000000001015",
    bridge: "0x0000000000000000000000000000000000001016",
    launchpad: "0x0000000000000000000000000000000000001017"
  }

  @market_signatures %{
    exchange: [
      "MarginDeposited(bytes32,bytes32,address,bytes32,uint256,bytes32,uint64)",
      "MarginWithdrawalRequested(bytes32,bytes32,address,bytes32,uint256,uint64)",
      "OrderPlaced(bytes32,bytes32,address,uint8,uint256,uint256,uint8,uint64)",
      "OrderCancelRequested(bytes32,bytes32,address,uint64)",
      "SettlementRequested(bytes32,bytes32,address,uint64)"
    ],
    bridge: [
      "BridgeIn(uint64,bytes32,address,uint64,address,uint256,string)",
      "BridgeOut(uint64,address,uint256,address,uint64)"
    ],
    launchpad: [
      "MarketCreated(address,address,string,string,string,uint8)",
      "Swap(address,address,address,bool,uint256,uint256,uint256,uint256)",
      "FeeRecorded(address,uint256,uint256,uint256)",
      "FeesClaimed(address,address,uint256)",
      "FeesBurned(address,uint256)",
      "FeeStrategyChanged(address,uint8,uint8)",
      "AirdropExecuted(address,uint256,uint256)",
      "AirdropClaimed(address,address,uint256,uint256)",
      "LpRewardsExecuted(address,uint256)",
      "PauseToggled(address,bool)"
    ]
  }

  describe "changeset/2" do
    test "accepts an exchange action" do
      %{block: block, transaction: transaction} = block_with_transaction()

      changeset = MarketEvent.changeset(%MarketEvent{}, attributes(block, transaction))

      assert changeset.valid?
      assert {:ok, event} = Repo.insert(changeset)
      assert event.domain == :exchange
      assert event.kind == "OrderPlaced"
      assert Decimal.equal?(event.amount, Decimal.new(2500))
      assert event.block_number == block.number
    end

    test "accepts a bridge action that names the asset by its EVM address" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs =
        block
        |> attributes(transaction)
        |> Map.merge(%{domain: :bridge, kind: "BridgeOut", asset_id: nil, asset_address_hash: address_hash()})

      assert {:ok, event} = %MarketEvent{} |> MarketEvent.changeset(attrs) |> Repo.insert()
      assert event.domain == :bridge
      assert event.kind == "BridgeOut"
      assert is_nil(event.asset_id)
      assert event.asset_address_hash == attrs.asset_address_hash
    end

    test "accepts a launchpad action that moves no amount" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs =
        block
        |> attributes(transaction)
        |> Map.merge(%{domain: :launchpad, kind: "PauseToggled", amount: nil})

      assert {:ok, event} = %MarketEvent{} |> MarketEvent.changeset(attrs) |> Repo.insert()
      assert event.domain == :launchpad
      assert is_nil(event.amount)
    end

    test "requires the key, the domain and the kind" do
      changeset = MarketEvent.changeset(%MarketEvent{}, %{})

      refute changeset.valid?

      errors = changeset_errors(changeset)

      assert errors[:transaction_hash] == ["can't be blank"]
      assert errors[:log_index] == ["can't be blank"]
      assert errors[:block_hash] == ["can't be blank"]
      assert errors[:block_number] == ["can't be blank"]
      assert errors[:domain] == ["can't be blank"]
      assert errors[:kind] == ["can't be blank"]
    end

    test "rejects a domain outside exchange, bridge and launchpad" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = block |> attributes(transaction) |> Map.put(:domain, :custody)

      changeset = MarketEvent.changeset(%MarketEvent{}, attrs)

      refute changeset.valid?
      assert {"is invalid", _} = changeset.errors[:domain]
      assert MarketEvent.domains() == [:exchange, :bridge, :launchpad]
    end

    test "rejects an empty kind" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = block |> attributes(transaction) |> Map.put(:kind, "")

      changeset = MarketEvent.changeset(%MarketEvent{}, attrs)

      refute changeset.valid?
      assert changeset_errors(changeset)[:kind] == ["can't be blank"]
    end

    test "rejects a negative amount without inserting a market row" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = block |> attributes(transaction) |> Map.put(:amount, -1)

      assert {:error, changeset} = %MarketEvent{} |> MarketEvent.changeset(attrs) |> Repo.insert()
      assert changeset_errors(changeset)[:amount] == ["must be greater than or equal to 0"]
      assert Repo.aggregate(MarketEvent, :count) == 0
    end
  end

  describe "native log definitions" do
    test "keeps every committed market ABI signature under its emitting precompile" do
      definitions = Enum.filter(PaxeerXLogs.definitions(), &(&1.table == :lx_market_events))

      assert definitions |> Enum.map(& &1.domain) |> Enum.uniq() |> Enum.sort() == Enum.sort(MarketEvent.domains())

      for {domain, signatures} <- @market_signatures do
        domain_definitions = Enum.filter(definitions, &(&1.domain == domain))

        assert Enum.sort(Enum.map(domain_definitions, & &1.signature)) == Enum.sort(signatures)

        for definition <- domain_definitions do
          assert definition.address == Map.fetch!(@precompiles, domain)
          assert definition.kind == Macro.underscore(definition.name)
        end
      end

      keys = Enum.map(definitions, &{&1.address, &1.topic})
      assert length(keys) == 17
      assert Enum.uniq(keys) == keys
    end

    test "every topic equals keccak256 of its canonical event signature" do
      definitions = PaxeerXLogs.definitions()
      refute definitions == []

      for definition <- definitions do
        signature =
          definition.name <>
            "(" <>
            Enum.map_join(definition.arguments, ",", fn {_name, type, _indexed?} -> abi_type(type) end) <> ")"

        assert definition.signature == signature
        assert definition.topic == hex(ExKeccak.hash_256(signature))
      end
    end

    test "keeps the market events key when there are no logs" do
      assert %{lx_market_events: [], lx_account_bindings: [], undecoded_log_count: 0} = PaxeerXLogs.parse([])
    end
  end

  describe "native log import" do
    setup do
      chain_type = Application.get_env(:explorer, :chain_type)
      Application.put_env(:explorer, :chain_type, :paxeer_x)
      on_exit(fn -> Application.put_env(:explorer, :chain_type, chain_type) end)
      :ok
    end

    test "decodes a BridgeOut log and writes its exact market row once" do
      %{block: block, transaction: transaction} = block_with_transaction()
      asset = <<0x41::160>>
      recipient = <<0x42::160>>

      log =
        market_log(
          block,
          transaction,
          7,
          :bridge,
          "BridgeOut(uint64,address,uint256,address,uint64)",
          [{{:uint, 64}, 7}, {:address, asset}, {{:uint, 64}, 512}],
          [{{:uint, 256}, 77}, {:address, recipient}]
        )

      assert %{lx_market_events: [row], lx_account_bindings: [], undecoded_log_count: 0} = PaxeerXLogs.parse([log])

      assert row == %{
               transaction_hash: transaction.hash,
               block_hash: block.hash,
               block_number: block.number,
               log_index: 7,
               event_name: "BridgeOut",
               domain: :bridge,
               kind: "bridge_out",
               address_hash: hex(recipient),
               account: nil,
               asset_id: nil,
               asset_address_hash: hex(asset),
               amount: 77,
               parameters: %{
                 "chain" => 7,
                 "asset" => hex(asset),
                 "amount" => 77,
                 "recipient" => hex(recipient),
                 "nonce" => 512
               }
             }

      options = %{paxeer_x_market_events: %{params: [row]}}
      assert {:ok, %{insert_paxeer_x_market_events: [_]}} = Import.all(options)
      assert {:ok, %{insert_paxeer_x_market_events: []}} = Import.all(options)
      assert [stored] = Repo.all(MarketEvent.only_consensus_query())
      assert stored.transaction_hash == transaction.hash
      assert stored.block_hash == block.hash
      assert stored.block_number == block.number
      assert stored.log_index == 7
      assert stored.block_consensus == true
      assert stored.domain == :bridge
      assert stored.kind == "bridge_out"
      assert stored.event_name == "BridgeOut"
      assert stored.parameters == row.parameters
      assert to_string(stored.address_hash) == hex(recipient)
      assert to_string(stored.asset_address_hash) == hex(asset)
      assert is_nil(stored.asset_id)
      assert Decimal.equal?(stored.amount, Decimal.new(77))
    end

    test "preserves a BridgeIn dynamic denomination and full uint256 amount through import" do
      %{block: block, transaction: transaction} = block_with_transaction()
      amount = Integer.pow(2, 255) + 7

      log =
        market_log(
          block,
          transaction,
          8,
          :bridge,
          "BridgeIn(uint64,bytes32,address,uint64,address,uint256,string)",
          [{{:uint, 64}, 7}, {{:bytes, 32}, <<0x31::256>>}, {:address, <<0x32::160>>}],
          [{{:uint, 64}, 4}, {:address, <<0x33::160>>}, {{:uint, 256}, amount}, {:string, "factory/pax1/asset"}]
        )

      assert %{lx_market_events: [row], undecoded_log_count: 0} = PaxeerXLogs.parse([log])

      assert row.parameters == %{
               "chain" => 7,
               "txHash" => hex(<<0x31::256>>),
               "recipient" => hex(<<0x32::160>>),
               "logIndex" => 4,
               "asset" => hex(<<0x33::160>>),
               "amount" => amount,
               "denom" => "factory/pax1/asset"
             }

      assert {:ok, %{insert_paxeer_x_market_events: [_]}} = Import.all(%{paxeer_x_market_events: %{params: [row]}})
      assert [stored] = Repo.all(MarketEvent)
      assert stored.parameters == row.parameters
      assert stored.domain == :bridge
      assert stored.kind == "bridge_in"
      assert stored.event_name == "BridgeIn"
      assert Decimal.equal?(stored.amount, Decimal.new(amount))
    end

    test "imports exchange margin and launchpad pause logs into their own domains" do
      %{block: block, transaction: transaction} = block_with_transaction()

      margin =
        market_log(
          block,
          transaction,
          9,
          :exchange,
          "MarginDeposited(bytes32,bytes32,address,bytes32,uint256,bytes32,uint64)",
          [{{:bytes, 32}, <<0x31::256>>}, {{:bytes, 32}, <<0x32::256>>}, {:address, <<0x33::160>>}],
          [{{:bytes, 32}, <<0x34::256>>}, {{:uint, 256}, 4000}, {{:bytes, 32}, <<0x35::256>>}, {{:uint, 64}, 3}]
        )

      pause =
        market_log(block, transaction, 10, :launchpad, "PauseToggled(address,bool)", [{:address, <<0x51::160>>}], [
          {:bool, true}
        ])

      assert %{lx_market_events: [margin_row, pause_row], undecoded_log_count: 0} = PaxeerXLogs.parse([margin, pause])

      assert {:ok, %{insert_paxeer_x_market_events: inserted}} =
               Import.all(%{paxeer_x_market_events: %{params: [margin_row, pause_row]}})

      assert length(inserted) == 2

      assert [margin_event, pause_event] = Repo.all(order_by(MarketEvent, [event], asc: event.log_index))
      assert margin_event.domain == :exchange
      assert margin_event.kind == "margin_deposited"
      assert margin_event.event_name == "MarginDeposited"

      assert margin_event.parameters == %{
               "intentId" => hex(<<0x31::256>>),
               "account" => hex(<<0x32::256>>),
               "owner" => hex(<<0x33::160>>),
               "assetId" => hex(<<0x34::256>>),
               "amount" => 4000,
               "depositId" => hex(<<0x35::256>>),
               "nonce" => 3
             }

      assert to_string(margin_event.address_hash) == hex(<<0x33::160>>)
      assert to_string(margin_event.account) == hex(<<0x32::256>>)
      assert to_string(margin_event.asset_id) == hex(<<0x34::256>>)
      assert Decimal.equal?(margin_event.amount, Decimal.new(4000))
      assert pause_event.domain == :launchpad
      assert pause_event.kind == "pause_toggled"
      assert pause_event.event_name == "PauseToggled"
      assert pause_event.parameters == %{"token" => hex(<<0x51::160>>), "paused" => true}
      assert to_string(pause_event.asset_address_hash) == hex(<<0x51::160>>)
      assert is_nil(pause_event.amount)
    end

    test "refuses market topics from another precompile or an ordinary contract" do
      %{block: block, transaction: transaction} = block_with_transaction()

      log =
        market_log(block, transaction, 11, :launchpad, "PauseToggled(address,bool)", [{:address, <<0x51::160>>}], [
          {:bool, true}
        ])

      assert %{lx_market_events: [], undecoded_log_count: 0} =
               PaxeerXLogs.parse([Map.put(log, :address_hash, hex(<<0x99::160>>))])

      assert capture_log(fn ->
               assert %{lx_market_events: [], undecoded_log_count: 1} =
                        PaxeerXLogs.parse([Map.put(log, :address_hash, @precompiles.exchange)])
             end) =~ "No definition for topic0"

      assert Repo.aggregate(MarketEvent, :count) == 0
    end

    test "refuses unknown topics and missing indexed or unindexed arguments" do
      %{block: block, transaction: transaction} = block_with_transaction()

      log =
        market_log(
          block,
          transaction,
          12,
          :bridge,
          "BridgeOut(uint64,address,uint256,address,uint64)",
          [{{:uint, 64}, 7}, {:address, <<0x41::160>>}, {{:uint, 64}, 512}],
          [{{:uint, 256}, 77}, {:address, <<0x42::160>>}]
        )

      invalid_logs = [
        {Map.put(log, :first_topic, hex(<<0::256>>)), "No definition for topic0"},
        {Map.put(log, :fourth_topic, nil), "missing an indexed topic"},
        {Map.put(log, :data, "0x"), "carries no data for its unindexed arguments"}
      ]

      for {invalid_log, message} <- invalid_logs do
        assert capture_log(fn ->
                 assert %{lx_market_events: [], undecoded_log_count: 1} = PaxeerXLogs.parse([invalid_log])
               end) =~ message
      end

      assert Repo.aggregate(MarketEvent, :count) == 0
    end
  end

  describe "run/3" do
    test "inserts one row per market log across the three domains" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = attributes(block, transaction)

      changes = [
        attrs,
        Map.merge(attrs, %{log_index: 11, domain: :bridge, kind: "BridgeOut"}),
        Map.merge(attrs, %{log_index: 12, domain: :launchpad, kind: "Swap"})
      ]

      assert {:ok, %{insert_paxeer_x_market_events: inserted}} = run_changes(changes)

      assert length(inserted) == 3

      assert [{:exchange, "OrderPlaced"}, {:bridge, "BridgeOut"}, {:launchpad, "Swap"}] ==
               MarketEvent
               |> order_by([event], asc: event.log_index)
               |> select([event], {event.domain, event.kind})
               |> Repo.all()
    end

    test "leaves the stored row untouched when the same log is imported again" do
      %{block: block, transaction: transaction} = block_with_transaction()

      attrs = attributes(block, transaction)

      assert {:ok, %{insert_paxeer_x_market_events: [_]}} = run_changes([attrs])

      assert {:ok, %{insert_paxeer_x_market_events: []}} =
               run_changes([Map.merge(attrs, %{kind: "OrderCancelRequested", amount: Decimal.new(1)})])

      assert [stored] = Repo.all(MarketEvent)
      assert stored.kind == "OrderPlaced"
      assert Decimal.equal?(stored.amount, Decimal.new(2500))
    end

    test "handles an empty changes list" do
      assert {:ok, %{insert_paxeer_x_market_events: []}} = run_changes([])
    end
  end

  describe "only_consensus_query/0" do
    test "drops the rows of a block that lost consensus" do
      %{block: kept_block, transaction: kept_transaction} = block_with_transaction()
      %{block: reorged_block, transaction: reorged_transaction} = block_with_transaction()

      assert {:ok, _} =
               run_changes([
                 attributes(kept_block, kept_transaction),
                 attributes(reorged_block, reorged_transaction)
               ])

      Repo.update!(Ecto.Changeset.change(reorged_block, consensus: false))

      assert [kept] = Repo.all(MarketEvent.only_consensus_query())
      assert kept.transaction_hash == kept_transaction.hash

      assert {1, nil} =
               [reorged_block.hash]
               |> MarketEvent.lose_consensus_query()
               |> Repo.update_all(set: [block_consensus: false])

      assert [false] =
               MarketEvent
               |> where([event], event.block_hash == ^reorged_block.hash)
               |> select([event], event.block_consensus)
               |> Repo.all()
    end
  end

  defp block_with_transaction do
    block = insert(:block)
    transaction = :transaction |> insert() |> with_block(block)

    %{block: block, transaction: transaction}
  end

  defp attributes(block, transaction) do
    %{
      transaction_hash: transaction.hash,
      log_index: 10,
      block_hash: block.hash,
      block_number: block.number,
      block_consensus: true,
      domain: :exchange,
      kind: "OrderPlaced",
      address_hash: address_hash(),
      account: block_hash(),
      asset_id: block_hash(),
      asset_address_hash: address_hash(),
      amount: Decimal.new(2500)
    }
  end

  defp run_changes(changes) when is_list(changes) do
    Multi.new()
    |> MarketEvents.run(changes, %{
      timeout: :infinity,
      timestamps: %{inserted_at: DateTime.utc_now(), updated_at: DateTime.utc_now()}
    })
    |> Repo.transaction()
  end

  defp market_log(block, transaction, index, domain, signature, indexed, unindexed) do
    [second_topic, third_topic, fourth_topic] =
      indexed |> Enum.map(&encode_arguments([&1])) |> Kernel.++([nil, nil, nil]) |> Enum.take(3)

    %{
      address_hash: Map.fetch!(@precompiles, domain),
      block_hash: block.hash,
      block_number: block.number,
      transaction_hash: transaction.hash,
      index: index,
      first_topic: hex(ExKeccak.hash_256(signature)),
      second_topic: second_topic,
      third_topic: third_topic,
      fourth_topic: fourth_topic,
      data: encode_arguments(unindexed)
    }
  end

  defp encode_arguments(arguments) do
    {types, values} = Enum.unzip(arguments)
    hex(TypeEncoder.encode(values, types))
  end

  defp hex(bytes), do: "0x" <> Base.encode16(bytes, case: :lower)
  defp abi_type(:address), do: "address"
  defp abi_type(:bool), do: "bool"
  defp abi_type(:string), do: "string"
  defp abi_type({:bytes, size}), do: "bytes#{size}"
  defp abi_type({:uint, bits}), do: "uint#{bits}"
end

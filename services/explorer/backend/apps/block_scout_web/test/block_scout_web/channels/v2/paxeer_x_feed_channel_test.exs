defmodule BlockScoutWeb.V2.PaxeerXFeedChannelTest do
  use BlockScoutWeb.ConnCase, async: false

  import Phoenix.ChannelTest, only: [socket: 3, subscribe_and_join: 2]

  alias BlockScoutWeb.Notifier
  alias BlockScoutWeb.V2.UserSocket
  alias Explorer.Chain.Cache.Counters.AverageBlockTime
  alias Explorer.Chain.PaxeerX.PriceFeed
  alias Phoenix.Socket.Broadcast

  @feeder "0xb97e177fe6e701db2cf85c1ba1307688166318cc"
  @feeder_bech32 "pax1h9lpwllxuuqakt8ctsd6zvrk3qtxxxxvyfs9jc"

  setup do
    PriceFeed.upsert_markets([
      %{market_id: 0, symbol: "BTC-USD", chainflow_symbol: "BTCUSDT"},
      %{market_id: 2, symbol: "SOL-USD", chainflow_symbol: "SOLUSDT"}
    ])

    start_supervised!(AverageBlockTime)
    Application.put_env(:explorer, AverageBlockTime, enabled: true, cache_period: 1_800_000)

    on_exit(fn ->
      Application.put_env(:explorer, AverageBlockTime, enabled: false, cache_period: 1_800_000)
    end)

    :ok
  end

  describe "price update broadcasts" do
    test "every inserted update is broadcast with the payload its REST item carries", %{conn: conn} do
      block = insert(:block, number: 20)
      insert(:block, number: 22)

      [first, second] =
        store(block, [
          submission(0, [observation(0, 5)]),
          submission(1, [observation(2, 7), observation(0, 6)])
        ])

      Enum.each(
        ~w(paxeer_x_feed:new_update paxeer_x_feed:market:0 paxeer_x_feed:market:2 transactions:new_transaction blocks:new_block),
        &@endpoint.subscribe/1
      )

      Notifier.handle_event({:chain_event, :paxeer_x_price_updates, :realtime, [20]})

      for hash <- [first, second] do
        assert_receive %Broadcast{topic: "paxeer_x_feed:new_update", event: "new_update", payload: payload}
        assert payload["hash"] == hash
        assert json(payload) == json_response(get(conn, "/api/v2/paxeer-x/feed/updates/#{hash}"), 200)
      end

      market_0 = json_response(get(conn, "/api/v2/paxeer-x/feed/markets/0/updates"), 200)["items"]
      assert [%{"update_hash" => ^second}, %{"update_hash" => ^first}] = market_0

      for item <- Enum.reverse(market_0) do
        assert_receive %Broadcast{topic: "paxeer_x_feed:market:0", event: "new_update", payload: payload}
        assert json(payload) == item
      end

      assert [market_2_item] = json_response(get(conn, "/api/v2/paxeer-x/feed/markets/2/updates"), 200)["items"]
      assert_receive %Broadcast{topic: "paxeer_x_feed:market:2", event: "new_update", payload: payload}
      assert json(payload) == market_2_item

      assert_receive %Broadcast{
        topic: "transactions:new_transaction",
        event: "transaction",
        payload: %{transaction: 2}
      }

      assert_receive %Broadcast{topic: "blocks:new_block", event: "new_block", payload: %{block: block_item}}
      assert block_item["height"] == 20
      assert block_item["price_update_count"] == 2
      assert json_response(get(conn, "/api/v2/blocks/20"), 200)["price_update_count"] == 2

      refute_receive %Broadcast{topic: "paxeer_x_feed:new_update"}
    end

    test "a block without updates broadcasts nothing" do
      insert(:block, number: 21)
      Enum.each(~w(paxeer_x_feed:new_update transactions:new_transaction blocks:new_block), &@endpoint.subscribe/1)

      Notifier.handle_event({:chain_event, :paxeer_x_price_updates, :realtime, [21]})

      refute_receive %Broadcast{}
    end
  end

  describe "join" do
    test "joins the update topic and a market topic" do
      for topic <- ["paxeer_x_feed:new_update", "paxeer_x_feed:market:3"] do
        assert {:ok, _reply, _socket} = UserSocket |> socket("no_id", %{}) |> subscribe_and_join(topic)
      end
    end

    test "refuses a market topic without a market id" do
      for topic <- ["paxeer_x_feed:market:abc", "paxeer_x_feed:market:-1", "paxeer_x_feed:other"] do
        assert {:error, %{reason: _}} = UserSocket |> socket("no_id", %{}) |> subscribe_and_join(topic)
      end
    end
  end

  defp store(block, submissions) do
    {:ok, _count} = PriceFeed.replace_block_updates(block, submissions)
    Enum.map(submissions, & &1.hash)
  end

  defp submission(index, observations) do
    %{
      hash: "0x" <> Base.encode16(:crypto.strong_rand_bytes(32), case: :lower),
      index: index,
      feeder_bech32: @feeder_bech32,
      feeder_hash: @feeder,
      status: 1,
      gas_used: 100_000,
      gas_wanted: 200_000,
      observations: observations
    }
  end

  defp observation(market_id, value) do
    %{
      market_id: market_id,
      sequence: value,
      index_price: value * 1_000_000_000_000_000_000,
      mark_price: (value + 1) * 1_000_000_000_000_000_000,
      source_timestamp_ms: 1_792_636_042_000 + value
    }
  end

  defp json(payload), do: payload |> Jason.encode!() |> Jason.decode!()
end

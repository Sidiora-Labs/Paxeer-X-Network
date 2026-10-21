defmodule Explorer.Chain.PaxeerX.CapabilitiesBootTest do
  @moduledoc """
  What the capability probe is allowed to do to the boot of the application.

  The process is a child of `Explorer.Supervisor`, and a supervisor starts its
  children one after another: anything this process does before it reports
  started is time the rest of the application, including the endpoint that binds
  the port, spends waiting. So the node is kept silent here, and the assertions
  are that starting is bounded anyway, that the state a caller reads while the
  node has not answered is the absent one rather than a failure, and that the
  first probe that does succeed fills it.
  """

  use Explorer.DataCase, async: false

  import Mox

  alias Explorer.Chain.PaxeerX.Capabilities

  @head "0x16c1320"

  @bytecode "0x60806040523480156100105760006000fd"

  # `(address, string, bytes32, bytes32)` with no binding: four head words and
  # then the empty `paxAddr`, which is what the addr precompile answers once it
  # carries the method.
  @unified_account_answer "0x" <>
                            String.duplicate("0", 64) <>
                            String.duplicate("0", 62) <>
                            "80" <>
                            String.duplicate("0", 64) <>
                            String.duplicate("0", 64) <> String.duplicate("0", 64)

  # The bound the start has to stay inside. A probe that ran before the process
  # reported started would hold it for @node_silence_milliseconds instead.
  @start_within_milliseconds 1_000

  # How long the silent node holds a request that reaches it.
  @node_silence_milliseconds :timer.seconds(30)

  # How long the assertions wait for something the process does on its own.
  @await_milliseconds :timer.seconds(15)

  @poll_interval_milliseconds 50

  # Short enough that the tick after the refused probe lands inside the test.
  @refresh_interval_seconds 1

  setup :set_mox_global
  setup :verify_on_exit!

  setup do
    previous = Application.get_env(:explorer, Capabilities)

    Application.put_env(:explorer, Capabilities,
      enabled: true,
      refresh_interval_seconds: @refresh_interval_seconds
    )

    on_exit(fn ->
      case previous do
        nil -> Application.delete_env(:explorer, Capabilities)
        configuration -> Application.put_env(:explorer, Capabilities, configuration)
      end
    end)

    :ok
  end

  describe "start_link/1 against a node that never answers" do
    test "reports started within a bounded time and publishes the probe as unavailable" do
      silent_node()

      {elapsed_microseconds, pid} = :timer.tc(fn -> start_supervised!(Capabilities) end)

      assert elapsed_microseconds < @start_within_milliseconds * 1_000

      # The node is only reached once the process is started, so the probe is
      # still outstanding at this point and the state has to answer without it.
      assert_receive :node_reached, @await_milliseconds

      snapshot = Capabilities.all()

      assert snapshot.checked_at == nil

      for surface <- Capabilities.surfaces() do
        assert Map.fetch!(snapshot, surface) == false
        refute Capabilities.live?(surface)
      end

      assert Process.alive?(pid)
    end

    test "fills its state from the first probe that succeeds" do
      node = silent_node()

      pid = start_supervised!(Capabilities)

      assert_receive :node_reached, @await_milliseconds
      assert Capabilities.all().checked_at == nil

      answer_from_now_on(node)
      refuse_held_request(pid)

      snapshot = await_probe(@await_milliseconds)

      assert %DateTime{} = snapshot.checked_at

      for surface <- Capabilities.surfaces() do
        assert Map.fetch!(snapshot, surface)
        assert Capabilities.live?(surface)
      end
    end
  end

  # A node that holds every request it is given instead of answering it, the way
  # a node behaves under a deployment's in-flight caps when it is saturated.
  # `refuse_held_request/1` ends the held request and `answer_from_now_on/1` puts
  # the node back in service.
  defp silent_node do
    test_process = self()
    # Started through the test supervisor so it outlives the capability
    # process, which is stopped before it.
    node = start_supervised!({Agent, fn -> :silent end})

    stub(EthereumJSONRPC.Mox, :json_rpc, fn
      %{method: "eth_blockNumber"}, _options ->
        case Agent.get(node, & &1) do
          :silent ->
            send(test_process, :node_reached)

            receive do
              :refuse -> {:error, :timeout}
            after
              @node_silence_milliseconds -> {:error, :timeout}
            end

          :answering ->
            {:ok, @head}
        end

      requests, _options when is_list(requests) ->
        {:ok, Enum.map(requests, &answer/1)}
    end)

    node
  end

  defp answer_from_now_on(node), do: Agent.update(node, fn _ -> :answering end)

  # Lets the held request end the way a request to a node that never answers
  # ends, so the probe that was outstanding fails and the next tick is the first
  # one that can succeed.
  defp refuse_held_request(pid), do: send(pid, :refuse)

  defp answer(%{id: id, method: "eth_getCode"}),
    do: %{id: id, jsonrpc: "2.0", result: @bytecode}

  defp answer(%{id: id, method: "eth_call"}),
    do: %{id: id, jsonrpc: "2.0", result: @unified_account_answer}

  defp await_probe(remaining_milliseconds) do
    case Capabilities.all() do
      %{checked_at: %DateTime{}} = snapshot ->
        snapshot

      _ when remaining_milliseconds <= 0 ->
        flunk("the process published no probe within #{@await_milliseconds} ms")

      _ ->
        Process.sleep(@poll_interval_milliseconds)
        await_probe(remaining_milliseconds - @poll_interval_milliseconds)
    end
  end
end

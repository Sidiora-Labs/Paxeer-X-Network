defmodule Indexer.Fetcher.PaxeerXKernelReceiptsTest do
  use ExUnit.Case, async: false

  alias Indexer.Fetcher.PaxeerXKernelReceipts

  @prefix "INDEXER_PAXEER_X_KERNEL_RECEIPTS_"
  @names ~w(RELAY_URL NETWORK_ID SEQUENCER_PUBLIC_KEY SEQUENCER_ID FIRST_BATCH LAST_BATCH CODEC INTERVAL_MS)
  @public_key String.duplicate("ab", 32)
  @sequencer_id String.duplicate("cd", 32)
  @invalid "invalid kernel receipt source, authority range or native codec"

  setup do
    saved = Map.new(@names, &{&1, System.get_env(@prefix <> &1)})
    codec = Path.join(System.tmp_dir!(), "kernel-receipt-codec-#{System.unique_integer([:positive])}")
    File.write!(codec, "")

    on_exit(fn ->
      File.rm(codec)
      put_env(saved)
    end)

    env = %{
      "RELAY_URL" => "https://relay.invalid/",
      "NETWORK_ID" => "7",
      "SEQUENCER_PUBLIC_KEY" => String.upcase(@public_key),
      "SEQUENCER_ID" => @sequencer_id,
      "FIRST_BATCH" => "1",
      "LAST_BATCH" => "100",
      "CODEC" => codec,
      "INTERVAL_MS" => nil
    }

    put_env(env)

    %{env: env, codec: codec}
  end

  describe "config_from_env/0" do
    test "is off while no relay is configured", %{env: env} do
      put_env(Map.put(env, "RELAY_URL", nil))
      assert PaxeerXKernelReceipts.config_from_env() == nil

      put_env(Map.put(env, "RELAY_URL", ""))
      assert PaxeerXKernelReceipts.config_from_env() == nil
    end

    test "pins an https relay, the authority range, the network, the sequencer and the codec", %{codec: codec} do
      config = PaxeerXKernelReceipts.config_from_env()

      assert config == %{
               relay_url: "https://relay.invalid",
               network_id: 7,
               sequencer_public_key: @public_key,
               sequencer_id: @sequencer_id,
               first_batch: 1,
               last_batch: 100,
               codec: codec,
               interval: 2_000
             }

      assert PaxeerXKernelReceipts.child_spec([config]) == %{
               id: PaxeerXKernelReceipts,
               start: {PaxeerXKernelReceipts, :start_link, [config, []]}
             }
    end

    test "admits plain http only towards a loopback relay", %{env: env} do
      for url <- ["http://127.0.0.1:8545", "http://localhost:8545", "http://[::1]:8545"] do
        put_env(Map.put(env, "RELAY_URL", url))
        assert PaxeerXKernelReceipts.config_from_env().relay_url == url
      end

      assert_refused(env, %{"RELAY_URL" => "http://relay.invalid"})
    end

    test "refuses credentials, a query, a fragment or a scheme other than http(s) in the relay url", %{env: env} do
      urls = [
        "https://operator@relay.invalid",
        "https://relay.invalid/?network=7",
        "https://relay.invalid/#batches",
        "wss://relay.invalid",
        "ftp://relay.invalid"
      ]

      for url <- urls do
        assert_refused(env, %{"RELAY_URL" => url})
      end
    end

    test "refuses an empty, inverted or out-of-range authority and a zero interval", %{env: env} do
      assert_refused(env, %{"FIRST_BATCH" => "0"})
      assert_refused(env, %{"FIRST_BATCH" => "10", "LAST_BATCH" => "9"})
      assert_refused(env, %{"FIRST_BATCH" => "9223372036854775808", "LAST_BATCH" => "9223372036854775808"})
      assert_refused(env, %{"LAST_BATCH" => "18446744073709551616"})
      assert_refused(env, %{"INTERVAL_MS" => "0"})

      put_env(Map.merge(env, %{"FIRST_BATCH" => "9223372036854775807", "LAST_BATCH" => "18446744073709551615"}))
      config = PaxeerXKernelReceipts.config_from_env()

      assert config.first_batch == 9_223_372_036_854_775_807
      assert config.last_batch == 18_446_744_073_709_551_615
    end

    test "refuses a network id outside 1..4294967295", %{env: env} do
      assert_refused(env, %{"NETWORK_ID" => "0"})
      assert_refused(env, %{"NETWORK_ID" => "4294967296"})

      put_env(Map.put(env, "NETWORK_ID", "4294967295"))
      assert PaxeerXKernelReceipts.config_from_env().network_id == 4_294_967_295
    end

    test "refuses a relative, missing or non-regular codec path", %{env: env, codec: codec} do
      assert_refused(env, %{"CODEC" => "bin/layerx-codec"})
      assert_refused(env, %{"CODEC" => codec <> ".missing"})
      assert_refused(env, %{"CODEC" => System.tmp_dir!()})
    end

    test "refuses a configured relay without its network pin", %{env: env} do
      put_env(Map.put(env, "NETWORK_ID", nil))

      error = assert_raise ArgumentError, fn -> PaxeerXKernelReceipts.config_from_env() end

      assert error.message == "#{@prefix}NETWORK_ID must be set when #{@prefix}RELAY_URL is set"
    end
  end

  defp assert_refused(env, overrides) do
    put_env(Map.merge(env, overrides))
    assert_raise ArgumentError, @invalid, fn -> PaxeerXKernelReceipts.config_from_env() end
  end

  defp put_env(values) do
    Enum.each(values, fn
      {name, nil} -> System.delete_env(@prefix <> name)
      {name, value} -> System.put_env(@prefix <> name, value)
    end)
  end
end

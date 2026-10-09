defmodule Indexer.ApplicationTest do
  use ExUnit.Case, async: false

  setup do
    standalone = Application.get_env(:nft_media_handler, :standalone_media_worker?)
    relay_url = System.get_env("INDEXER_PAXEER_X_KERNEL_RECEIPTS_RELAY_URL")

    on_exit(fn ->
      Application.put_env(:nft_media_handler, :standalone_media_worker?, standalone)

      if relay_url do
        System.put_env("INDEXER_PAXEER_X_KERNEL_RECEIPTS_RELAY_URL", relay_url)
      else
        System.delete_env("INDEXER_PAXEER_X_KERNEL_RECEIPTS_RELAY_URL")
      end
    end)

    :ok
  end

  describe "start/2" do
    test "supervises nothing on a media worker node, kernel receipt relay settings left unread" do
      Application.put_env(:nft_media_handler, :standalone_media_worker?, true)
      System.put_env("INDEXER_PAXEER_X_KERNEL_RECEIPTS_RELAY_URL", "https://relay.invalid")

      assert Explorer.mode() == :media_worker
      assert {:ok, supervisor} = Indexer.Application.start(:normal, [])
      assert Process.whereis(Indexer.Application) == supervisor
      assert Supervisor.which_children(supervisor) == []

      :ok = Supervisor.stop(supervisor)
    end
  end
end

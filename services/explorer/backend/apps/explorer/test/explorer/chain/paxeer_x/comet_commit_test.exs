defmodule Explorer.Chain.PaxeerX.CometCommitTest do
  use ExUnit.Case, async: true

  import Bitwise

  alias Explorer.Chain.PaxeerX.CometCommit

  @fixture Path.expand("../../../support/fixture/paxeer_x/comet_commit.json", __DIR__)

  setup_all do
    fixture = @fixture |> File.read!() |> Jason.decode!()
    {:ok, fixture: fixture, chain_id: fixture["chain_id"]}
  end

  test "a commit signed by all four validators verifies", %{fixture: fixture, chain_id: chain_id} do
    %{"commit" => commit, "validators" => validators} = fixture["four_of_four"]

    assert CometCommit.verify(chain_id, commit, validators) == {:ok, 1_000}
    assert CometCommit.verify(chain_id, commit["signed_header"], validators) == {:ok, 1_000}
  end

  test "a commit signed by two of four equal-power validators is refused", %{fixture: fixture, chain_id: chain_id} do
    %{"commit" => commit, "validators" => validators} = fixture["two_of_four"]

    assert CometCommit.verify(chain_id, commit, validators) == {:error, :insufficient_voting_power}
  end

  test "a flipped signature byte is refused", %{fixture: fixture, chain_id: chain_id} do
    %{"commit" => commit, "validators" => validators} = fixture["four_of_four"]

    flipped =
      update_in(commit, ["signed_header", "commit", "signatures"], fn signatures ->
        List.update_at(signatures, 0, fn signature ->
          <<first, rest::binary>> = Base.decode64!(signature["signature"])
          %{signature | "signature" => Base.encode64(<<bxor(first, 0x01), rest::binary>>)}
        end)
      end)

    assert CometCommit.verify(chain_id, flipped, validators) == {:error, {:invalid_signature, 0}}
  end

  test "a changed header field is refused", %{fixture: fixture, chain_id: chain_id} do
    %{"commit" => commit, "validators" => validators} = fixture["four_of_four"]

    changed =
      update_in(commit, ["signed_header", "header", "app_hash"], fn _ -> String.duplicate("0A", 32) end)

    assert CometCommit.verify(chain_id, changed, validators) == {:error, :header_hash_mismatch}
  end

  test "a changed validator power is refused", %{fixture: fixture, chain_id: chain_id} do
    %{"commit" => commit, "validators" => validators} = fixture["four_of_four"]

    changed =
      update_in(validators, ["validators"], fn list ->
        List.update_at(list, 0, &%{&1 | "voting_power" => "11"})
      end)

    assert CometCommit.verify(chain_id, commit, changed) == {:error, :validators_hash_mismatch}
  end

  test "a commit for another chain id is refused", %{fixture: fixture} do
    %{"commit" => commit, "validators" => validators} = fixture["four_of_four"]

    assert CometCommit.verify("hyperpax_126-1", commit, validators) == {:error, :chain_id_mismatch}
  end
end

defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.SettlementRung do
  @moduledoc """
  This module defines the schema for the rung an item sits on in the Paxeer X Network
  settlement ladder.
  """
  require OpenApiSpex

  OpenApiSpex.schema(%{
    title: "PaxeerXSettlementRung",
    description:
      "The rung the settlement ladder puts an item on: `submitted` while neither a committed block " <>
        "nor a verified sequencer receipt holds it, `receipted` once a sequencer receipt passes the " <>
        "client checks R1-R6 against the pinned receipt keys, and `committed` once a block the " <>
        "committed head covers holds it.",
    type: :string,
    enum: ["submitted", "receipted", "committed"],
    nullable: false
  })
end

defmodule BlockScoutWeb.Schemas.API.V2.PaxeerX.UnifiedAccountTest do
  use ExUnit.Case, async: true

  alias BlockScoutWeb.Schemas.API.V2.PaxeerX.UnifiedAccount

  test "requires every property of the one-account view and admits no other" do
    schema = UnifiedAccount.schema()

    assert schema.required == [
             :identities,
             :balances,
             :activity,
             :next_page_params,
             :page_cursor,
             :first_page_cursor,
             :page_number,
             :activity_total
           ]

    assert Enum.sort(Map.keys(schema.properties)) == Enum.sort(schema.required)
    assert schema.additionalProperties == false
    assert schema.nullable == false
    assert schema.properties.page_number.minimum == 1
    assert schema.properties.activity_total.nullable == true
  end
end

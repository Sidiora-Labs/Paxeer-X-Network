defmodule Explorer.Repo.Migrations.AddPaxeerXPriceFeedScanStatus do
  use Ecto.Migration

  def change do
    alter table(:paxeer_x_price_feed_scanned_blocks) do
      add(:status, :text, null: false, default: "scanned")
      add(:error, :text)
    end
  end
end

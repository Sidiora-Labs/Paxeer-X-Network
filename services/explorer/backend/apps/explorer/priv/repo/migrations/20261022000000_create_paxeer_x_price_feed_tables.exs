defmodule Explorer.Repo.Migrations.CreatePaxeerXPriceFeedTables do
  use Ecto.Migration

  def change do
    create table(:paxeer_x_price_updates, primary_key: false) do
      add(:hash, :bytea, null: false, primary_key: true)
      add(:block_number, :bigint, null: false)
      add(:block_hash, references(:blocks, column: :hash, on_delete: :delete_all, type: :bytea), null: false)
      add(:index, :integer, null: false)
      add(:feeder_hash, :bytea, null: false)
      add(:feeder_bech32, :text, null: false)
      add(:status, :integer, null: false)
      add(:gas_used, :numeric, precision: 100, null: false)
      add(:gas_wanted, :numeric, precision: 100, null: false)
      add(:observation_count, :integer, null: false)
      add(:timestamp, :utc_datetime_usec, null: false)

      timestamps(null: false, type: :utc_datetime_usec)
    end

    create(index(:paxeer_x_price_updates, ["block_number DESC", "index DESC"]))
    create(index(:paxeer_x_price_updates, [:feeder_hash]))
    create(index(:paxeer_x_price_updates, [:block_hash]))

    create table(:paxeer_x_price_observations, primary_key: false) do
      add(
        :update_hash,
        references(:paxeer_x_price_updates, column: :hash, on_delete: :delete_all, type: :bytea),
        null: false,
        primary_key: true
      )

      add(:market_id, :integer, null: false, primary_key: true)
      add(:block_number, :bigint, null: false)
      add(:sequence, :bigint, null: false)
      add(:index_price, :numeric, precision: 78, scale: 0, null: false)
      add(:mark_price, :numeric, precision: 78, scale: 0, null: false)
      add(:source_timestamp_ms, :bigint, null: false)
    end

    create(index(:paxeer_x_price_observations, [:market_id, "block_number DESC"]))

    create table(:paxeer_x_price_markets, primary_key: false) do
      add(:market_id, :integer, null: false, primary_key: true)
      add(:symbol, :text, null: false)
      add(:chainflow_symbol, :text, null: false)
      add(:refreshed_at, :utc_datetime_usec, null: false)
    end

    create table(:paxeer_x_price_feed_scanned_blocks, primary_key: false) do
      add(:block_number, :bigint, null: false, primary_key: true)
    end
  end
end

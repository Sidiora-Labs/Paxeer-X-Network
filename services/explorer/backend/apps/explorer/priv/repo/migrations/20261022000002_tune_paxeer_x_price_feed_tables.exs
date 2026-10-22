defmodule Explorer.Repo.Migrations.TunePaxeerXPriceFeedTables do
  use Ecto.Migration
  @disable_ddl_transaction true
  @disable_migration_lock true

  @heap_parameters [
    {"paxeer_x_price_observations",
     [
       {"autovacuum_vacuum_scale_factor", "0.0"},
       {"autovacuum_vacuum_threshold", "200000"},
       {"autovacuum_vacuum_insert_scale_factor", "0.0"},
       {"autovacuum_vacuum_insert_threshold", "1000000"},
       {"autovacuum_analyze_scale_factor", "0.0"},
       {"autovacuum_analyze_threshold", "500000"},
       {"autovacuum_vacuum_cost_limit", "4000"},
       {"vacuum_truncate", "false"}
     ]},
    {"paxeer_x_price_updates",
     [
       {"autovacuum_vacuum_scale_factor", "0.0"},
       {"autovacuum_vacuum_threshold", "20000"},
       {"autovacuum_vacuum_insert_scale_factor", "0.0"},
       {"autovacuum_vacuum_insert_threshold", "50000"},
       {"autovacuum_analyze_scale_factor", "0.0"},
       {"autovacuum_analyze_threshold", "25000"},
       {"vacuum_truncate", "false"}
     ]},
    {"paxeer_x_price_feed_scanned_blocks",
     [
       {"autovacuum_vacuum_scale_factor", "0.0"},
       {"autovacuum_vacuum_threshold", "10000"},
       {"autovacuum_vacuum_insert_scale_factor", "0.0"},
       {"autovacuum_vacuum_insert_threshold", "10000"},
       {"autovacuum_analyze_scale_factor", "0.0"},
       {"autovacuum_analyze_threshold", "10000"}
     ]}
  ]

  def up do
    create_if_not_exists(
      index(:paxeer_x_price_updates, [:feeder_hash, "block_number DESC", "index DESC"], concurrently: true)
    )

    drop_if_exists(index(:paxeer_x_price_updates, [:feeder_hash], concurrently: true))

    execute("""
    ALTER TABLE paxeer_x_price_feed_scanned_blocks
      ADD CONSTRAINT paxeer_x_price_feed_scanned_blocks_status_check
      CHECK (status IN ('scanned', 'unavailable', 'failed')) NOT VALID
    """)

    execute(
      "ALTER TABLE paxeer_x_price_feed_scanned_blocks VALIDATE CONSTRAINT paxeer_x_price_feed_scanned_blocks_status_check"
    )

    Enum.each(@heap_parameters, fn {table, parameters} ->
      execute(
        "ALTER TABLE #{table} SET (#{Enum.map_join(parameters, ", ", fn {name, value} -> "#{name} = #{value}" end)})"
      )
    end)
  end

  def down do
    Enum.each(@heap_parameters, fn {table, parameters} ->
      execute("ALTER TABLE #{table} RESET (#{Enum.map_join(parameters, ", ", fn {name, _value} -> name end)})")
    end)

    execute(
      "ALTER TABLE paxeer_x_price_feed_scanned_blocks DROP CONSTRAINT IF EXISTS paxeer_x_price_feed_scanned_blocks_status_check"
    )

    create_if_not_exists(index(:paxeer_x_price_updates, [:feeder_hash], concurrently: true))

    drop_if_exists(
      index(:paxeer_x_price_updates, [:feeder_hash, "block_number DESC", "index DESC"], concurrently: true)
    )
  end
end

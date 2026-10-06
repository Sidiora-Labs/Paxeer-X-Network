# Applies the deploy overlay named by BRIDGE_DEPLOY_OVERLAY to one committed
# chain configuration; bridge/deploy/chainconfig/overlay.go is the same rule in
# Go. Called as: jq --slurpfile overlay <overlay.json> -f overlay.jq <config.json>
if ($overlay | length) != 1 or ($overlay[0] | type) != "object"
then error("the overlay is one JSON object") else . end
| ["ethereum", "base", "arbitrum", "optimism", "bnb", "polygon", "avalanche", "hyperevm", "solana"] as $known
| ($overlay[0].chains // {}) as $chains
| (($chains | keys) - $known) as $unknown
| if ($unknown | length) > 0 then error("chains.\($unknown[0]): not a bridge chain") else . end
| ($chains[.chain] // {}) as $entry
| if $entry.owner then .owner = $entry.owner else . end
| if $entry.deployer then .deployer = $entry.deployer else . end
| if (($entry.attestors // []) | length) > 0 then .attestors = $entry.attestors else . end
| if $entry.threshold then .threshold = $entry.threshold else . end
| if $entry.environment then .environment = $entry.environment else . end
| reduce (($entry.caps // {}) | to_entries[]) as $cap (.;
    if any(.assets[]; .symbol == $cap.key)
    then .assets |= map(if .symbol == $cap.key
        then .per_tx_cap = $cap.value.per_tx_cap | .total_cap = $cap.value.total_cap
        else . end)
    else error("caps.\($cap.key): the overlay caps an asset the chain does not list") end)
| if $entry.big_blocks_acknowledged == true
  then (if .big_blocks then .big_blocks.acknowledged = true
        else error("big_blocks_acknowledged: only hyperevm needs the deploying account switched to big blocks") end)
  else . end
| if $entry.program_id
  then (if .solana then .solana.program_id = $entry.program_id
        else error("program_id: only the Solana chain holds custody in a program") end)
  else . end

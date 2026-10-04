use std::path::Path;
use std::process::Command;

const COMPLETE: &str = "MIGRATION_V2_CONNECTED_COMPLETE ethereum=1 solana=1 funded=2 skipped=0";

#[test]
fn migration_v2_connected_runs_authenticated_mapping_and_funded_settlement() {
    let fixture = std::env::var_os("LAYERX_MIGRATION_V2_FIXTURE")
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| panic!("genuine protected migration V2 fixture required"));
    assert!(
        Path::new(&fixture).is_absolute(),
        "absolute owner fixture required"
    );
    let root = Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .unwrap_or_else(|| panic!("repository root unavailable"));
    let output = Command::new("python3")
        .arg(root.join("tools/qualification/paxeer-x/migration-v2.py"))
        .current_dir(root)
        .env("LAYERX_MIGRATION_V2_FIXTURE", fixture)
        .output()
        .unwrap_or_else(|_| panic!("genuine connected migration process unavailable"));
    println!(
        "MIGRATION_V2_CONNECTED_SUBPROCESS exit={}",
        output
            .status
            .code()
            .map_or_else(|| "signal".to_owned(), |code| code.to_string())
    );
    assert_eq!(output.status.code(), Some(0), "connected migration refused");
    let report = std::str::from_utf8(&output.stdout)
        .unwrap_or_else(|_| panic!("connected migration report encoding refused"));
    assert_eq!(
        report.lines().filter(|line| *line == COMPLETE).count(),
        1,
        "complete genuine Ethereum/Solana funded coverage required"
    );
    let records: Vec<&str> = report
        .lines()
        .filter(|line| line.starts_with("PAXEER_X_GATE "))
        .collect();
    assert_eq!(records.len(), 1, "one actual coverage record required");
    let count = records[0]
        .strip_prefix("PAXEER_X_GATE tests=")
        .and_then(|value| value.strip_suffix(" skipped=0"))
        .and_then(|value| value.parse::<u64>().ok())
        .filter(|value| *value > 0)
        .unwrap_or_else(|| panic!("nonzero real cases and zero skipped cases required"));
    println!("{COMPLETE}");
    println!("PAXEER_X_GATE tests={count} skipped=0");
}

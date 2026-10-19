use layerx_gas_station::config::{ConfigError, ServiceConfig};
use layerx_gas_station::journal::Journal;
use layerx_gas_station::price::PaymasterRateSource;
use layerx_gas_station::rate::{
    next_wait, PublisherConfig, RatePublisher, RateRefusal, DAY_SECONDS,
};
use layerx_gas_station::rpc::{ConfiguredRpc, HttpsExchange, JsonRpc};
use layerx_gas_station::service::{drive, Limits, Schedule, Service};
use layerx_gas_station::signer::LocalSigner;
use layerx_gas_station::station::{GasStation, StationError};
use std::ffi::OsString;
use std::io::{self, Write};
use std::net::{SocketAddr, TcpListener};
use std::path::PathBuf;
use std::process::ExitCode;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

#[derive(Debug, Eq, PartialEq)]
struct Arguments {
    config: PathBuf,
    journal: PathBuf,
}

fn arguments(arguments: impl IntoIterator<Item = OsString>) -> Option<Arguments> {
    let mut arguments = arguments.into_iter();
    let (config_flag, config) = (arguments.next()?, arguments.next()?);
    let (journal_flag, journal) = (arguments.next()?, arguments.next()?);
    (config_flag == "--config"
        && journal_flag == "--journal"
        && !config.is_empty()
        && !journal.is_empty()
        && arguments.next().is_none())
    .then(|| Arguments {
        config: PathBuf::from(config),
        journal: PathBuf::from(journal),
    })
}

#[derive(Debug, Eq, PartialEq)]
struct RateArguments {
    config: PathBuf,
    journal: PathBuf,
    rate_file: PathBuf,
}

fn rate_arguments(arguments: impl IntoIterator<Item = OsString>) -> Option<RateArguments> {
    let mut arguments = arguments.into_iter();
    let mut value = |flag: &str| {
        let (name, value) = (arguments.next()?, arguments.next()?);
        (name == flag && !value.is_empty()).then(|| PathBuf::from(value))
    };
    let parsed = RateArguments {
        config: value("--config")?,
        journal: value("--journal")?,
        rate_file: value("--rate-file")?,
    };
    arguments.next().is_none().then_some(parsed)
}

#[derive(Debug, Eq, PartialEq)]
struct CheckArguments {
    config: PathBuf,
    rate_config: PathBuf,
}

fn check_arguments(arguments: impl IntoIterator<Item = OsString>) -> Option<CheckArguments> {
    let mut arguments = arguments.into_iter();
    let mut value = |flag: &str| {
        let (name, value) = (arguments.next()?, arguments.next()?);
        (name == flag && !value.is_empty()).then(|| PathBuf::from(value))
    };
    let parsed = CheckArguments {
        config: value("--config")?,
        rate_config: value("--rate-config")?,
    };
    arguments.next().is_none().then_some(parsed)
}

/// Loads the station configuration and the publisher configuration exactly as
/// the two processes do and returns every refusal, each with the flag naming
/// its file; reads no key variable, opens no journal, makes no network call.
fn check_configs(arguments: &CheckArguments) -> Vec<(&'static str, ConfigError)> {
    let station = ServiceConfig::load(&arguments.config)
        .err()
        .map(|error| ("--config", error));
    let publisher = PublisherConfig::load(&arguments.rate_config)
        .err()
        .map(|error| ("--rate-config", error));
    station.into_iter().chain(publisher).collect()
}

fn run_check(arguments: Option<&CheckArguments>) -> ExitCode {
    let Some(arguments) = arguments else {
        eprintln!("Paxeer X Network gas station configuration check");
        eprintln!("usage: paxeer-gas-station check --config PATH --rate-config PATH");
        return ExitCode::from(2);
    };
    let refusals = check_configs(arguments);
    for (flag, error) in &refusals {
        eprintln!("check {flag}: {error}");
    }
    if refusals.is_empty() {
        ExitCode::SUCCESS
    } else {
        ExitCode::from(2)
    }
}

fn run_rate(arguments: &RateArguments) -> ExitCode {
    let config = match PublisherConfig::load(&arguments.config) {
        Ok(config) => config,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::from(2);
        }
    };
    let signer = match LocalSigner::from_env(&config.owner_key_env) {
        Ok(signer) => signer,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    let rpc = match ConfiguredRpc::new(&config.station, HttpsExchange) {
        Ok(rpc) => rpc,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::from(2);
        }
    };
    let journal = match Journal::open(&arguments.journal) {
        Ok(journal) => journal,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    let daily_wei = config.daily_wei_ceiling().unwrap_or(u128::MAX);
    let mut publisher =
        match RatePublisher::new(config, signer, rpc, journal, Duration::from_secs(2)) {
            Ok(publisher) => publisher,
            Err(error) => {
                eprintln!("{error}");
                return ExitCode::FAILURE;
            }
        };
    let cadence = publisher.cadence();
    println!(
        "Paxeer X Network rate publisher publishing every {cadence} s, at most {daily_wei} wei per chain day"
    );
    loop {
        let result = publisher.publish(&arguments.rate_file);
        match &result {
            Ok(publication) => {
                let state = publisher.journal().state();
                let day = publication.signed_at / DAY_SECONDS;
                let kind = if state
                    .rate_transactions
                    .get(&publication.hash)
                    .is_some_and(|tx| tx.cancellation)
                {
                    "cancelled"
                } else {
                    "published"
                };
                println!(
                    "rate {kind} nonce={} hash={} rate={} cost_wei={} spent_wei={} spent_gas={} reserved_wei={} daily_wei_max={daily_wei}",
                    publication.nonce,
                    layerx_gas_station::rpc::hex(&publication.hash),
                    u128::from_be_bytes(publication.rate[16..].try_into().unwrap_or([0; 16])),
                    state
                        .publications
                        .get(&publication.hash)
                        .and_then(|(_, settled)| *settled)
                        .map_or(0, |settled| settled.cost_wei),
                    state.publication_wei(day),
                    state.publication_gas(day),
                    state.reserved_wei(),
                );
            }
            Err(RateRefusal::Unchanged { .. }) => {}
            Err(
                error @ (RateRefusal::Journal(_)
                | RateRefusal::LegacyUnresolved
                | RateRefusal::NotOwner),
            ) => {
                eprintln!("{error}");
                return ExitCode::FAILURE;
            }
            Err(error) => eprintln!("{error}"),
        }
        std::thread::sleep(Duration::from_secs(next_wait(cadence, &result)));
    }
}

fn unix_time() -> Option<u64> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .ok()
        .map(|elapsed| elapsed.as_secs())
}

fn main() -> ExitCode {
    if std::env::args_os().nth(1).is_some_and(|a| a == "check") {
        return run_check(check_arguments(std::env::args_os().skip(2)).as_ref());
    }
    if std::env::args_os().nth(1).is_some_and(|a| a == "rate") {
        let Some(arguments) = rate_arguments(std::env::args_os().skip(2)) else {
            eprintln!("Paxeer X Network rate publisher");
            eprintln!(
                "usage: paxeer-gas-station rate --config PATH --journal PATH --rate-file PATH"
            );
            return ExitCode::from(2);
        };
        return run_rate(&arguments);
    }
    let Some(arguments) = arguments(std::env::args_os().skip(1)) else {
        eprintln!("Paxeer X Network gas station");
        eprintln!("usage: paxeer-gas-station --config PATH --journal PATH");
        eprintln!("       paxeer-gas-station check --config PATH --rate-config PATH");
        return ExitCode::from(2);
    };
    let config = match ServiceConfig::load(&arguments.config) {
        Ok(config) => config,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::from(2);
        }
    };
    let signer = match LocalSigner::from_config(&config.station) {
        Ok(signer) => signer,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    let (rpc, rates) = match (
        ConfiguredRpc::new(&config.station, HttpsExchange),
        ConfiguredRpc::new(&config.station, HttpsExchange),
    ) {
        (Ok(rpc), Ok(rates)) => (
            rpc,
            PaymasterRateSource::new(rates, config.station.paymaster),
        ),
        (Err(error), _) | (_, Err(error)) => {
            eprintln!("{error}");
            return ExitCode::from(2);
        }
    };
    let journal = match Journal::open(&arguments.journal) {
        Ok(journal) => journal,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    if let Err(error) = rpc.set_deadline(Some(Instant::now() + Duration::from_secs(20))) {
        eprintln!("{error}");
        return ExitCode::FAILURE;
    }
    let station = match GasStation::new(config.station.clone(), signer, rpc, rates, journal) {
        Ok(station) => station,
        Err(error @ StationError::Invalid) => {
            eprintln!("{error}");
            return ExitCode::from(2);
        }
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    let unresolved = station.unresolved().len();
    let mut service = Service::new(&config, station, unix_time, Limits::default());
    let schedule = Schedule::default();
    let recovery = match service.recover(schedule.budget) {
        Ok(recovery) => recovery,
        Err(error) => {
            eprintln!("startup recovery {error}");
            return ExitCode::FAILURE;
        }
    };
    println!(
        "startup recovery unresolved={unresolved} completed={} pending={} unreachable={} deferred={}",
        recovery.completed, recovery.pending, recovery.unreachable, recovery.deferred
    );
    let Ok(listener) = TcpListener::bind(config.listen) else {
        eprintln!("listen address unavailable");
        return ExitCode::FAILURE;
    };
    if let Err(error) = report_listening(io::stdout().lock(), config.listen) {
        eprintln!("{error}");
        return ExitCode::FAILURE;
    }
    match drive(&listener, &mut service, schedule, &mut io::stderr().lock()) {
        Ok(never) => match never {},
        Err(error) => {
            eprintln!("{error}");
            ExitCode::FAILURE
        }
    }
}

fn report_listening(mut output: impl Write, listen: SocketAddr) -> io::Result<()> {
    writeln!(
        output,
        "Paxeer X Network gas station ready, serving POST /quote, /submit, /status and /retry on {listen}"
    )?;
    output.flush()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn listening_report_names_the_endpoints_and_address() -> io::Result<()> {
        let mut output = Vec::new();
        report_listening(&mut output, SocketAddr::from(([127, 0, 0, 1], 8545)))?;
        assert_eq!(
            output,
            b"Paxeer X Network gas station ready, serving POST /quote, /submit, /status and /retry on 127.0.0.1:8545\n"
        );
        let mut full = [];
        assert_eq!(
            report_listening(
                full.as_mut_slice(),
                SocketAddr::from(([127, 0, 0, 1], 8545))
            )
            .err()
            .map(|e| e.kind()),
            Some(io::ErrorKind::WriteZero)
        );
        Ok(())
    }

    #[test]
    fn exact_config_and_journal_arguments_required() {
        assert_eq!(
            arguments(
                [
                    "--config",
                    "station.json",
                    "--journal",
                    "state/sponsorship.jsonl"
                ]
                .map(OsString::from)
            ),
            Some(Arguments {
                config: PathBuf::from("station.json"),
                journal: PathBuf::from("state/sponsorship.jsonl"),
            })
        );
        for args in [
            vec![],
            vec!["--config"],
            vec!["--config", "station.json"],
            vec!["--config", "station.json", "--journal"],
            vec!["--journal", "state.jsonl", "--config", "station.json"],
            vec!["--other", "station.json", "--journal", "state.jsonl"],
            vec!["--config", "", "--journal", "state.jsonl"],
            vec!["--config", "station.json", "--journal", ""],
            vec![
                "--config",
                "station.json",
                "--journal",
                "state.jsonl",
                "extra",
            ],
        ] {
            assert_eq!(arguments(args.into_iter().map(OsString::from)), None);
        }
    }

    #[test]
    fn exact_rate_arguments_required() {
        assert_eq!(
            rate_arguments(
                [
                    "--config",
                    "rate.json",
                    "--journal",
                    "rate.jsonl",
                    "--rate-file",
                    "rate.toml"
                ]
                .map(OsString::from)
            ),
            Some(RateArguments {
                config: PathBuf::from("rate.json"),
                journal: PathBuf::from("rate.jsonl"),
                rate_file: PathBuf::from("rate.toml"),
            })
        );
        for args in [
            vec![],
            vec!["--config", "rate.json", "--journal", "rate.jsonl"],
            vec![
                "--journal",
                "rate.jsonl",
                "--config",
                "rate.json",
                "--rate-file",
                "rate.toml",
            ],
            vec![
                "--config",
                "rate.json",
                "--journal",
                "rate.jsonl",
                "--rate-file",
                "",
            ],
            vec![
                "--config",
                "rate.json",
                "--journal",
                "rate.jsonl",
                "--rate-file",
                "rate.toml",
                "extra",
            ],
        ] {
            assert_eq!(rate_arguments(args.into_iter().map(OsString::from)), None);
        }
    }

    #[test]
    fn exact_check_arguments_required() {
        assert_eq!(
            check_arguments(
                ["--config", "station.json", "--rate-config", "rate.json"].map(OsString::from)
            ),
            Some(CheckArguments {
                config: PathBuf::from("station.json"),
                rate_config: PathBuf::from("rate.json"),
            })
        );
        for args in [
            vec![],
            vec!["--config", "station.json"],
            vec!["--config", "station.json", "--rate-config"],
            vec!["--rate-config", "rate.json", "--config", "station.json"],
            vec!["--config", "station.json", "--journal", "rate.json"],
            vec!["--config", "", "--rate-config", "rate.json"],
            vec!["--config", "station.json", "--rate-config", ""],
            vec![
                "--config",
                "station.json",
                "--rate-config",
                "rate.json",
                "extra",
            ],
        ] {
            assert_eq!(check_arguments(args.into_iter().map(OsString::from)), None);
        }
    }

    /// One pinned receipt key in the shape `px_getReceiptKeys` returns.
    fn receipt_key(key_id: u8, public_key: [u8; 32], first: u64, last: u64) -> serde_json::Value {
        use base64::Engine as _;
        let encode = |bytes: [u8; 32]| base64::engine::general_purpose::STANDARD.encode(bytes);
        serde_json::json!({"keyId": encode([key_id; 32]), "publicKey": encode(public_key),
            "firstHeight": first.to_string(), "lastHeight": last.to_string()})
    }

    #[test]
    fn check_refuses_what_the_publisher_refuses() -> Result<(), Box<dyn std::error::Error>> {
        const PAYMASTER: &str = "0x4444444444444444444444444444444444444444";
        let refused = |flag: &'static str, field: &'static str| (flag, ConfigError { field });
        std::env::set_var("GAS_STATION_PAYMASTER", PAYMASTER);
        let dir =
            std::env::temp_dir().join(format!("paxeer-gas-station-check-{}", std::process::id()));
        std::fs::create_dir_all(&dir)?;
        let arguments = CheckArguments {
            config: dir.join("station.json"),
            rate_config: dir.join("rate.json"),
        };
        let shared = serde_json::json!({"max_priority_fee_per_gas":1_000_000_000_u64,
            "chain_id":1325,"endpoints":["https://paxeer.app"],"paymaster":PAYMASTER,
            "token":"0x21f7b20a555199fa73A238B1a91FD0f549068fEe","decimals":6,
            "max_rate_age":300,"spread_bps":500,"margin_bps":100,"per_account_limit":4_000_000,
            "per_interval_limit":8_000_000,"per_quote_limit":3_000_000,"interval_seconds":60,
            "balance_floor":100,"relayer_key_env":"GAS_STATION_RELAYER_KEY"});
        let mut station = shared.clone();
        station["listen"] = "[::]:8080".into();
        station["gas_limit"] = 1_000_000.into();
        let mut publisher = shared;
        publisher["rate_owner_key_env"] = "GAS_STATION_RATE_OWNER_KEY".into();
        publisher["rate_cadence_seconds"] = 120.into();
        publisher["rate_gas_budget_per_day"] = 1_000_000.into();
        publisher["rate_balance_floor"] = 1_000_000_000_000_000_u64.into();
        publisher["rate_max_fee_per_gas"] = 5_000_000_000_u64.into();
        publisher["rate_confirmation_retry_seconds"] = 60.into();
        let key = ed25519_dalek::SigningKey::from_bytes(&[0x17; 32])
            .verifying_key()
            .to_bytes();
        let weak: [u8; 32] = std::array::from_fn(|index| u8::from(index == 0));
        let write = |station: &serde_json::Value, keys: serde_json::Value| {
            let mut rate = publisher.clone();
            rate["sequencer_receipt_keys"] = keys;
            std::fs::write(&arguments.config, format!("{station}\n"))?;
            std::fs::write(&arguments.rate_config, format!("{rate}\n"))
        };

        write(
            &station,
            serde_json::json!([receipt_key(0x5a, key, 1, 256)]),
        )?;
        assert_eq!(check_configs(&arguments), vec![]);
        write(
            &station,
            serde_json::json!([
                receipt_key(0x5b, key, 1, 15),
                receipt_key(0x5a, key, 16, 256)
            ]),
        )?;
        assert_eq!(check_configs(&arguments), vec![]);

        for keys in [
            serde_json::json!([]),
            serde_json::json!([receipt_key(0x5a, weak, 1, 256)]),
            serde_json::json!([
                receipt_key(0x5a, key, 1, 15),
                receipt_key(0x5a, key, 16, 256)
            ]),
            serde_json::json!([receipt_key(0x5a, key, 257, 256)]),
        ] {
            write(&station, keys.clone())?;
            assert_eq!(
                check_configs(&arguments),
                vec![refused("--rate-config", "sequencer_receipt_keys")],
                "{keys}"
            );
        }

        let mut malformed = station.clone();
        malformed["listen"] = "[::]:0".into();
        write(
            &malformed,
            serde_json::json!([receipt_key(0x5a, key, 1, 256)]),
        )?;
        assert_eq!(
            check_configs(&arguments),
            vec![refused("--config", "listen")]
        );
        write(&malformed, serde_json::json!([]))?;
        assert_eq!(
            check_configs(&arguments),
            vec![
                refused("--config", "listen"),
                refused("--rate-config", "sequencer_receipt_keys")
            ]
        );
        std::fs::write(&arguments.config, "{")?;
        assert_eq!(
            check_configs(&arguments),
            vec![
                refused("--config", "config"),
                refused("--rate-config", "sequencer_receipt_keys")
            ]
        );

        std::fs::remove_dir_all(&dir)?;
        assert_eq!(
            check_configs(&arguments),
            vec![
                refused("--config", "config_path"),
                refused("--rate-config", "config_path")
            ]
        );
        Ok(())
    }

    #[test]
    fn clock_reads_unix_seconds() {
        let before = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|elapsed| elapsed.as_secs())
            .ok();
        let now = unix_time();
        assert!(now.is_some());
        assert!(now >= before);
    }
}

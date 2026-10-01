use layerx_gas_station::config::ServiceConfig;
use layerx_gas_station::journal::Journal;
use layerx_gas_station::price::PaymasterRateSource;
use layerx_gas_station::rate::{PublisherConfig, RatePublisher, RateRefusal, DAY_SECONDS};
use layerx_gas_station::rpc::{ConfiguredRpc, HttpsExchange};
use layerx_gas_station::service::{serve, Limits, Service};
use layerx_gas_station::signer::LocalSigner;
use layerx_gas_station::station::{GasStation, StationError};
use std::ffi::OsString;
use std::io::{self, Write};
use std::net::{SocketAddr, TcpListener};
use std::path::PathBuf;
use std::process::ExitCode;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

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
        let wait = match publisher.publish(&arguments.rate_file) {
            Ok(publication) => {
                let state = publisher.journal().state();
                let day = publication.signed_at / DAY_SECONDS;
                println!(
                    "rate published nonce={} hash={} rate={} cost_wei={} spent_wei={} spent_gas={} reserved_wei={} daily_wei_max={daily_wei}",
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
                cadence
            }
            Err(RateRefusal::Unchanged { age }) => cadence.saturating_sub(age).max(1),
            Err(error @ RateRefusal::Journal(_)) => {
                eprintln!("{error}");
                return ExitCode::FAILURE;
            }
            Err(error) => {
                eprintln!("{error}");
                cadence
            }
        };
        std::thread::sleep(Duration::from_secs(wait));
    }
}

fn unix_time() -> Option<u64> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .ok()
        .map(|elapsed| elapsed.as_secs())
}

fn main() -> ExitCode {
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
    let Ok(listener) = TcpListener::bind(config.listen) else {
        eprintln!("listen address unavailable");
        return ExitCode::FAILURE;
    };
    let mut service = Service::new(&config, station, unix_time, Limits::default());
    if let Err(error) = report_listening(io::stdout().lock(), config.listen) {
        eprintln!("{error}");
        return ExitCode::FAILURE;
    }
    match serve(&listener, &mut service, &mut io::stderr().lock()) {
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
        "Paxeer X Network gas station serving POST /quote and POST /submit on {listen}"
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
            b"Paxeer X Network gas station serving POST /quote and POST /submit on 127.0.0.1:8545\n"
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

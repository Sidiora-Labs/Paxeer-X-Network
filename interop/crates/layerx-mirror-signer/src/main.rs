//! Reference mirror signer daemon.
//!
//! Loads the Ethereum and Solana publisher keys of the `layerx-mirror-signer`
//! secret and answers `interop/deploy/mirror/signer-protocol.md` on an
//! owner-only Unix domain socket the co-located publisher container reaches.
//! Started as `layerx-mirror-signer bridge`, it instead loads the keys of the
//! bridge key manifest and answers the co-located bridge relayer.

use std::process::ExitCode;

use layerx_mirror_signer::{BridgeOptions, Options, SignerListener};

fn main() -> ExitCode {
    let mut arguments = std::env::args().skip(1).peekable();
    if arguments.peek().map(String::as_str) == Some("bridge") {
        arguments.next();
        return bridge(arguments);
    }
    let options = match Options::parse(arguments) {
        Ok(options) => options,
        Err(error) => {
            eprintln!("layerx-mirror-signer: {error}");
            return ExitCode::FAILURE;
        }
    };
    let listener = match SignerListener::bind(&options) {
        Ok(listener) => listener,
        Err(error) => {
            eprintln!("layerx-mirror-signer: {error}");
            return ExitCode::FAILURE;
        }
    };
    if listener.service().serves_solana() {
        eprintln!(
            "layerx-mirror-signer: listening on {} for {} (secp256k1) and {} (ed25519)",
            listener.socket().display(),
            options.ethereum_key_handle,
            options.solana_key_handle
        );
    } else {
        eprintln!(
            "layerx-mirror-signer: listening on {} for {} (secp256k1); no Solana publisher key at {}, so {} is not served",
            listener.socket().display(),
            options.ethereum_key_handle,
            options.solana_keypair_file.display(),
            options.solana_key_handle
        );
    }
    listener.serve()
}

fn bridge<I: Iterator<Item = String>>(arguments: I) -> ExitCode {
    let listener = match BridgeOptions::parse(arguments)
        .and_then(|options| SignerListener::bind_bridge(&options))
    {
        Ok(listener) => listener,
        Err(error) => {
            eprintln!("layerx-mirror-signer bridge: {error}");
            return ExitCode::FAILURE;
        }
    };
    eprintln!(
        "layerx-mirror-signer bridge: listening on {} for {}",
        listener.socket().display(),
        listener.service().handles().join(", ")
    );
    listener.serve()
}

use std::env;
use std::path::PathBuf;

use layerx_human_boundary_check::{
    human_boundary_policy, human_dependency_policy, human_payload_authority_gate,
    human_payload_bundle_gate, report,
};

fn main() {
    let mut arguments = env::args_os().skip(1);
    let first = arguments.next();
    if first.as_deref().is_some_and(|value| value == "--web-bundle") {
        let bundle = arguments
            .next()
            .map_or_else(|| PathBuf::from("apps/web/.next"), PathBuf::from);
        if !report(human_payload_bundle_gate(&bundle)) {
            std::process::exit(1);
        }
        println!("human web bundle carries no payload-encoding code path");
        return;
    }
    let root = first.map_or_else(|| PathBuf::from("services/human/crates"), PathBuf::from);
    if !report(human_dependency_policy(&root))
        || !report(human_boundary_policy(&root))
        || !report(human_payload_authority_gate(&root))
    {
        std::process::exit(1);
    }
    println!("human dependency, boundary, and payload-authority policies passed");
}

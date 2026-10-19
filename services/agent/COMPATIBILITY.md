# LayerX Agent Compatibility

This matrix is a release gate. A row means the daemon accepts the listed node-interface
major at the stated minimum additive minor, consumes that contract version, and preserves
the same public guarantees through the listed SDK version. Protocol facts still come only
from the node and must pass `layerx-proof`; compatibility never permits local reconstruction.

| Daemon | Node interface | Contract | SDK |
| --- | --- | --- | --- |
| 0.1.0 | 1.0+ | 1 | 0.1.0 |

The approval SDK module is an additive contract `1.1` feature. Clients negotiating contract
`1.0` must treat approval operations and lifecycle events as unavailable. The feature is
daemon-enforced and confers no protocol authority.

The `agent-test-agentd-migration` gate validates this row in Rust and then runs the LNI
boundary conformance program against the repository-built `layerxd` executable.

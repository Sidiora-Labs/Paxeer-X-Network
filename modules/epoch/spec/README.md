## Abstract

The epoch module signals other modules once every epoch. An epoch is a fixed period of time (one minute by default) measured from genesis; at each epoch boundary the module calls the `AfterEpochEnd` and `BeforeEpochStart` hooks registered by other modules and emits a `new_epoch` event.

## Contents

The module has no messages and no parameters. Its state, queries, hooks, and events are described in the module [README](../README.md).

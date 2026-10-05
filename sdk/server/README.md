# Server

The `server` package provides the mechanisms necessary to start an ABCI
Tendermint application in process, and the CLI framework (based on cobra,
`github.com/spf13/cobra`) necessary to fully bootstrap it. Its core functions
are `StartCmd` and `ExportCmd`, which create the commands that start the
application and export its state. `AddCommands` adds those two, a
`tendermint` command group, `version` and `rollback` to an application's root
command.

`paxd` wires this package in [`daemon/paxd/cmd/root.go`](../../daemon/paxd/cmd/root.go).

## Preliminary

The root command of an application typically is constructed with:

+ the command to start the application binary
+ the meta commands `query` and `tx`, plus auxiliary commands such as `init`,
  `keys` and the genesis commands.

It is vital that the root command of an application uses the
`PersistentPreRunE()` cobra command property for executing the command, so
all child commands have access to the server and client contexts. These
contexts are set as their default values initially and may be modified,
scoped to the command, in their respective `PersistentPreRunE()` functions.
Note that the `client.Context` is typically pre-populated with "default"
values that may be useful for all commands to inherit and override if
necessary.

Shape of the `paxd` root command:

```go
rootCmd := &cobra.Command{
	Use:   "paxd",
	Short: "Start Paxeer app",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		initClientCtx, err := client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags())
		if err != nil {
			return err
		}
		initClientCtx, err = config.ReadFromClientConfig(initClientCtx)
		if err != nil {
			return err
		}
		if err := client.SetCmdClientContextHandler(initClientCtx, cmd); err != nil {
			return err
		}

		customAppTemplate, customAppConfig := initAppConfig()

		return server.InterceptConfigsPreRunHandler(cmd, customAppTemplate, customAppConfig)
	},
}
```

The `SetCmdClientContextHandler` call reads persistent flags via
`ReadPersistentCommandFlags`, which creates a `client.Context` and sets that
on the root command's `Context`.

The `InterceptConfigsPreRunHandler` call creates a viper literal, a default
`server.Context` and a logger, and sets them on the root command's `Context`.
The `server.Context` will be modified and saved to disk via the internal
`interceptConfigs` call, which either reads or creates a Tendermint
configuration based on the home path provided. In addition,
`interceptConfigs` also reads and loads the application configuration,
`app.toml` (written from the custom template and config the application
passes in), and binds that to the `server.Context` viper literal. This is
vital so the application can get access to not only the CLI flags, but also
to the application configuration values provided by this file.

## `StartCmd`

`StartCmd(appCreator, defaultNodeHome, tracerProviderOptions)` accepts an
`AppCreator` function which returns an `Application`:

```go
type AppCreator func(dbm.DB, io.Writer, *tmcfg.Config, AppOptions) Application
```

The `AppCreator` is responsible for constructing the application based on the
options provided to it via `AppOptions`. The `AppOptions` interface type
defines a single method, `Get(string) interface{}`, and is implemented as the
viper literal that exists in the `server.Context`. All the possible options an
application may use and provide to the construction process are defined by
the `StartCmd` and by the application's config file, `app.toml`.

The application is always started in process with Tendermint. External ABCI
process support via socket or gRPC has been removed.

Under the hood, `StartCmd` will call `GetServerContextFromCmd`, which provides
the command access to a `server.Context`. This context provides access to the
viper literal, the Tendermint config and logger. This allows flags to be bound
to the viper literal and passed to the application construction.

The `paxd` `AppCreator` is `newApp` in
[`daemon/paxd/cmd/root.go`](../../daemon/paxd/cmd/root.go). It starts like
this:

```go
func newApp(
	db dbm.DB,
	traceStore io.Writer,
	tmConfig *tmcfg.Config,
	appOpts servertypes.AppOptions,
) servertypes.Application {
	var cache sdk.MultiStorePersistentCache

	if cast.ToBool(appOpts.Get(server.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}

	skipUpgradeHeights := make(map[int64]bool)
	for _, h := range cast.ToIntSlice(appOpts.Get(server.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}

	pruningOpts, err := server.GetPruningOptionsFromFlags(appOpts)
	if err != nil {
		panic(err)
	}
	// ...
}
```

Note, some of the options provided are exposed via CLI flags in the start
command and some are also allowed to be set in the application's `app.toml`.
It is recommended to use the `cast` package for type safety guarantees and due
to the limitations of CLI flag types.

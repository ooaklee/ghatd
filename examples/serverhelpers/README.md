# Offline server composition example

Run from the repository root:

```sh
go run ./examples/serverhelpers
go test ./examples/serverhelpers
```

The example resolves typed configuration, registers an acquired store with its
borrower's drain, constructs disabled IndexNow and sitemap policy, serves one
in-memory health request, and closes resources before telemetry. It uses no
credentials, database, listener or external notification. Expected output:

```text
health=204 public-pricing=true
telemetry stopped
```

Production composition adds actual storage and domain owners explicitly; it must
validate routes before starting workers or accepting requests. See the canonical
[serverhelpers guide](../../external/serverhelpers/README.md) for ownership and
failure semantics and each subpackage for its typed configuration.

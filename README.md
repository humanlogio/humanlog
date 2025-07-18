# humanlog

Read logs from `stdin` and prints them back to `stdout`, but prettier. It's also a localhost observability platform, supporting logs and distributed traces.

# Using it

```bash
curl -sSL "https://humanlog.io/install.sh" | bash
```

Read more at 👉 [how to install](https://humanlog.io/docs/get-started/installation) in our docs.

# Getting started

You can use `humanlog` in the CLI to make your logs prettier. You can also use it as a localhost observability platform to be able to explore the logs and traces that you ingested. Logs parsed by humanlog are persisted in a local database. Similary, traces are ingested and persisted in a local database when sent via OpenTelemetry/OTLP.

Read more at 👉 [how to get started](https://humanlog.io/docs/get-started/basic-usage) in our docs.

## Example - pretty-printing logs

If you emit logs in JSON or in [`logfmt`](https://brandur.org/logfmt), you will enjoy pretty logs when those
entries are encountered by `humanlog`. Unrecognized lines are left unchanged.

```bash
$ humanlog < /var/log/logfile.log
```

![example CLI usage](https://github.com/user-attachments/assets/d313b4df-30d4-423e-8ea4-f7a34e4e2c60)

When the logs are parsed, they are persisted in a local database. You can then query and explore them via the web UI.

## Example - using it as a localhost observability platform

With the query engine turned on (this is the default on new installations), you can query the logs and traces that you ingested. Run a query with:

```bash
$ humanlog query "spans | where duration > 100ms"
```

![querying](https://github.com/user-attachments/assets/74530872-fa9e-4c31-8798-99fdb34c7280)

Learn more at 👉 [how to query](https://humanlog.io/docs/features/query) in our docs.

## Example - ingesting traces

You can ingest traces via OpenTelemetry/OTLP. Using your language of choices' OpenTelemetry client will work out of the box using the default configuration. If not, point your exporter to `http://localhost:4317` or `http://localhost:4318` (these are the default values).

Other valid values for `OTEL_EXPORTER_OTLP_ENDPOINT` are:

| Situation                            | `OTEL_EXPORTER_OTLP_ENDPOINT`      |
| ------------------------------------ | ---------------------------------- |
| Default (same host, OTLP/HTTP)       | `http://localhost:4317`            |
| Default (same host, OTLP/gRPC)       | `http://localhost:4318`            |
| Docker, Orbstack, `kind` (OTLP/HTTP) | `http://host.docker.internal:4317` |
| Docker, Orbstack, `kind` (OTLP/gRPC) | `http://host.docker.internal:4318` |

Learn more at 👉 [ingesting OpenTelemetry/OTLP](https://humanlog.io/docs/integrations/opentelemetry) in our docs.

# Support and help

Open an issue on this repo or contribute a PR if you find a bug or want to add a feature in the CLI.

Need more help or want to give feedback? Join our [community channels](https://humanlog.io/support).

# License

The core CLI is MIT licensed (everything in this repo). Official releases are available at [humanlog.io](https://humanlog.io) and include proprietary code that is inserted at build time (the query engine).

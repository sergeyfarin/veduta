# Migration guide

## Importing from Homepage

Veduta imports Homepage services, bookmarks, settings, global widget options, and supported Docker labels. The importer maps groups to sections, services to cards, widget URLs to deduplicated connections, and known widget types to integrations. Unsupported widgets remain visible as link-only cards or produce a warning for manual configuration.

Create and validate a minimal Veduta configuration first. The importer updates this existing file; it does not create a backup. Copy the file before applying an import:

```sh
cp veduta.yaml veduta.yaml.before-homepage
./veduta import homepage --dir /path/to/homepage/config --config veduta.yaml
./veduta --check-config --config veduta.yaml
```

Use `--plugin-dir` if integration sources should be written relative to a directory other than `plugins`:

```sh
./veduta import homepage \
  --dir /path/to/homepage/config \
  --config /etc/veduta/veduta.yaml \
  --plugin-dir /opt/veduta/plugins
```

The edit preserves comments and YAML style where possible, validates the complete primary-plus-`conf.d` configuration, preserves the primary file's mode, and atomically replaces it only after validation succeeds.

The importer never copies Homepage key values into YAML. It writes deterministic `${secret:HOMEPAGE_*_KEY}` references and prints the environment variable names that must be supplied. Put each value in `/run/secrets/NAME` or the process environment.

Imported HTTP connections and external integrations are disabled. Review their base URLs, allowed paths, authentication shape, plugin source, and card bindings before changing `enabled` to `true`. Then inspect and approve external integration permissions:

```sh
./veduta integration list --config veduta.yaml
./veduta integration diff immich --config veduta.yaml
./veduta integration approve immich --config veduta.yaml
```

Read every warning. “Without widgets” means the service was retained as a link card. “Need manual configuration” means no safe automatic mapping exists. Compare the resulting dashboard with Homepage before removing the old deployment. To roll back the configuration, stop Veduta and restore `veduta.yaml.before-homepage`; keep any `conf.d` files consistent with that version.

## Upgrading Veduta configuration and data

The current configuration contract is `version: 1`. Validate configuration with the new binary before switching the running service:

```sh
./veduta-new --check-config --config /etc/veduta/veduta.yaml
```

Back up `veduta.yaml`, `conf.d`, `veduta.lock.yaml`, and the complete data directory before an upgrade. Database migrations run automatically when the new process opens SQLite and are forward-only; a newer database is not guaranteed to work with an older binary. Rollback therefore means restoring both the earlier binary and its matching backup.

Keep the binary, embedded frontend, first-party plugin manifests/modules, and approved lock entries on the same release. In practice that means replacing the binary and the first-party plugin directory (`./plugins` beside a relocatable install, `/usr/share/veduta/plugins` in the system layout and the container image) as one step, then restarting: a manifest whose digest no longer matches its lock entry is refused rather than run, so a half-applied upgrade fails safely but unhelpfully. Integrations you wrote or vendored yourself belong outside that directory — it is replaced wholesale — and are unaffected unless their own manifests changed. After upgrading, check `/healthz`, the configuration status, integration status, connection tests, representative cards, rules, and notification delivery before retiring the backup.

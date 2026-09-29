# Host metrics without SSH

Veduta supports Glances and Beszel over HTTP. The monitored host does not need an SSH account or
key for Veduta. For a Proxmox VE cluster — whose nodes report their own CPU, memory and storage
through the Proxmox API, so no per-host agent is needed at all — see
[docs/integrations.md](integrations.md).

Run Glances with its API and web UI disabled:

```sh
docker run -d --restart unless-stopped --name glances -p 61208:61208 --pid host \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  nicolargo/glances:latest-full glances -w --disable-webui
```

Glances binds all interfaces without authentication by default. Restrict port 61208 to a trusted
network, or configure Glances Basic/JWT authentication and the matching Veduta HTTP connection.
The `glances` plugin reads only `/api/4/cpu`, `/mem`, `/fs`, `/uptime`, `/network`, and `/sensors`.

Its card draws CPU and memory over the last 24 hours. That chart comes from what Veduta itself has
retained for the card - one reading per refresh, kept for 30 days by default - not from Glances,
so it starts empty on a new card and fills in as it runs; a stretch when the card could not reach
Glances shows as a gap. Glances 0.2.0 added the chart, so a lock approved for 0.1.0 needs
`veduta integration approve glances` again.

For Beszel, point an HTTP connection at the Hub and configure a read-only PocketBase bearer token.
Declare `plugins/beszel`, then set the card’s `systemId` parameter to the system record ID. The
plugin requests only that system record’s `name,status,info,updated` fields. Beszel’s compact
`info` object supplies CPU, memory, disk, network, temperature, and uptime values.

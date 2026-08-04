# Protocol Library security contract

Protocol Library stores imported client configuration material so the panel can regenerate Mihomo YAML. That material can include passwords, UUIDs, WireGuard private/pre-shared keys, and protocol-specific authentication values.

## API boundaries

- List, normal get, import/update responses, and preview are redacted. They must not return raw credentials.
- `GET .../:id/reveal` and `GET .../export.yaml` are explicit secret-bearing operations. They require normal panel/API authentication and must be treated like credential export.
- Mieru entries are import/display metadata only. They are not emitted into the managed Mihomo block because this build does not claim Mihomo support for Mieru.

## Storage and backups

Protocol connection secrets are stored in the panel database in recoverable form. Application-layer encryption at rest is **not** currently provided. Protect the database, its containing volume, snapshots, and backups as credentials:

- restrict filesystem ownership and permissions to the panel service account/root;
- do not publish or attach the database volume to unrelated containers;
- encrypt host disks and backup repositories where the threat model requires it;
- never paste database dumps, reveal responses, or exported YAML into logs or issue reports.

API redaction does not make a copied database or backup safe to disclose.

## Validation limits

A successful Mihomo configuration test (`mihomo -t`) proves that the generated YAML parses. It does not prove that a remote endpoint is reachable or that traffic traverses the selected proxy. Operational acceptance still requires a bounded connection test with DNS and external HTTP/IP verification against a live endpoint.

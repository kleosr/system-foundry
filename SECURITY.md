# Security policy

Report a vulnerability through GitHub private vulnerability reporting on this repository. Do not open a public issue for it.

Include the affected path, the steps to reproduce, and the impact. We answer within 7 days.

## Boundaries this project defends

- Tenant isolation: RLS in Postgres, tenant-scoped streams, the cache, the order views, and cost rollups.
- Agent role: read-only in the application and in Postgres (`agent_reader`).
- Tokens: ES256 with fixed issuer and audience. Refresh rotation revokes the family on reuse. The database stores sessions and codes only as hashes.
- Logs and traces: the redactor masks emails, phone numbers, SSNs, card numbers, and bearer secrets.

Demo accounts exist only in local mode. Shared mode refuses `FOUNDRY_DEMO_PASSWORD`.

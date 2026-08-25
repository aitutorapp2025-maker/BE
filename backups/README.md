# Database backups

PostgreSQL dumps of the `vaha_ai` database (plain SQL, `pg_dump --no-owner --no-privileges`).

**Secrets are NOT in these files:** the `settings` table's DATA is excluded
(`--exclude-table-data=settings`) because it holds live credentials — Razorpay
key secret + webhook secret, SMTP password, Anthropic/Voyage API keys, captcha
secret and the FCM service-account JSON. The table's schema IS included, and
the app re-creates a default settings row at boot; re-enter the credentials in
admin Settings after a restore (they are also backed up locally OUTSIDE the
repo, together with the full dump — see below).

A COMPLETE dump (including settings) is kept locally at
`<project root>/db-backups/` — never commit that one.

## Restore

```powershell
# fresh database
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -h 127.0.0.1 -U postgres -c "CREATE DATABASE vaha_ai_restore;"
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -h 127.0.0.1 -U postgres -d vaha_ai_restore -f .\vaha_ai_2026-08-25.sql
```

Then start the backend against it (it auto-migrates any newer columns) and
re-enter the provider credentials in Settings.

Note: `book_chunks` (pgvector embeddings) restore only onto a server with the
`vector` extension installed; on a server without it, remove that section from
the dump — books can be re-indexed from the admin panel instead.

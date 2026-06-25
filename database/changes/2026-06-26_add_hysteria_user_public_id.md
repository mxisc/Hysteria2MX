# Add Hysteria User Public ID

Purpose: hide sequential internal user IDs from public subscription URLs.

Scope: adds `hysteria_users.public_id`, backfills existing users with `usr_` plus random-looking hex text, and adds a unique index.

Execution order: run `database/migrations/2026-06-26_add_hysteria_user_public_id.sql` during normal panel migration startup before serving subscription links.

Rollback: remove or stop using public subscription URLs, then drop `uniq_hysteria_users_public_id` and `public_id` if a full rollback is required.

Validation: confirm existing users have non-empty unique `public_id` values and subscription URLs use `/subscription/usr_...` instead of `/subscription/1`.

Risks: old numeric subscription links become invalid because subscriptions now resolve by public ID. Users need to copy the new subscription link from the panel.

## Reusable Notes

Keep this file short. Add or expand a note only when it changes future behavior across tasks. Prefer merging one-off failures into broader rules instead of adding new incident logs.

### Public Docs And Repository Hygiene

- Problem: public docs drift into maintainer notes, stale naming, or mixed deployment flows. Trigger: README/deployment edits after product or runtime changes. Rule: use `Hysteria2MX`, keep README concise for open-source users, and separate production deployment from development deployment.
- Problem: public files expose private environment details. Trigger: copying live deployment examples, IDE links, CI snippets, or local paths into tracked docs/config. Rule: use placeholders such as `panel.example.com`, repository-relative paths, generic CI variables, and no real domains, IPs, accounts, secrets, production paths, host fingerprints, or `file:///Users/...` links.
- Problem: release snapshots accumulate history, debug bundles, runtime state, or duplicate docs. Trigger: preparing a public mirror/release. Rule: keep only required public docs, remove tracked debug/release/runtime artifacts, keep CI provider files generic, and use an orphan clean snapshot only when private git history must be removed.

### Project Shape And API Boundaries

- Problem: deployment or migration work reintroduces legacy runtime assumptions. Trigger: touching PHP migration remnants, deploy docs, or runtime entrypoints. Rule: target new installs as Nginx/Caddy + Go panel + MySQL; treat old PHP only as migration reference until its whole HTTP/CLI/job chain can be soft-deleted together.
- Problem: Go API work breaks the frontend even when a visible page seems done. Trigger: migrating or refactoring backend routes. Rule: audit `web/src/services/hysteriaService.ts`, public machine-to-machine routes, jobs, audit/notification APIs, `database/schema.sql`, and `web/src/types.ts` as one contract.
- Problem: subscription behavior regresses across clients or node counts. Trigger: changing user records, subscription links, public subscription routes, or node deletion. Rule: keep public subscription IDs non-enumerable, support raw and Clash/Mihomo formats, sync subscribable user templates across eligible nodes, and never let node deletion cascade-remove users without a safe migration/blocking path.
- Problem: top-level layout becomes hard to reason about. Trigger: adding sources, templates, binaries, or assets. Rule: keep `web/` for frontend source, `cmd/` for Go entrypoints, `internal/panel/` for backend core, `deploy/systemd/` for service templates, `artifacts/agent/` for Agent binaries, and `public/` for built web output.

### Config, Setup, And Settings

- Problem: first install becomes ambiguous or unsafe. Trigger: setup flow, schema import, or database initialization changes. Rule: support clean first install only, reject reused databases with existing system tables, validate admin usernames consistently, and run shared migrations from web/CLI with `schema_migrations`.
- Problem: runtime paths break under `build/panel/`, release layouts, or shared config. Trigger: resolving schema, static files, SSH helpers, systemd templates, or `panel.env`. Rule: resolve resources from persisted config, then the real panel executable/release root, then cwd as last fallback; keep `PANEL_STATIC_DIR` stable and relative when possible.
- Problem: mutable settings drift between env, defaults, database, localStorage, and live services. Trigger: changing branding, SMTP, login protection, login background, mock mode, or `public_api_base_url`. Rule: keep only bootstrap essentials in env, store operator settings in `system_settings`, expose safe public bootstrap settings, and update one shared `*Config` used by all long-lived services.
- Problem: public URL handling produces localhost or malformed remote links. Trigger: Agent bootstrap, Hysteria auth, subscription generation, or mock helpers. Rule: store API base URLs canonically with `/api`, derive site-root subscription links separately under `/subscription/`, and never confuse site root with API root.
- Problem: login encryption breaks across builds/releases. Trigger: frontend build-time seed injection or setup writing runtime seeds. Rule: do not inject a long-lived frontend login seed; use `/api/auth/login-challenge` to return per-request derived data from runtime config and nonce.

### Security And User-Facing Messages

- Problem: UI/API responses leak implementation details or sensitive data. Trigger: forwarding raw errors, setup failures, SQL/SSH issues, paths, stack traces, topology, raw API payloads, or credentials to clients. Rule: return safe user-facing messages; keep detailed diagnostics only in controlled logs and redact sensitive fields.
- Problem: users get excessive permissions or filesystem access. Trigger: editing role maps, log/settings pages, SSH key handling, or node forms. Rule: keep viewer roles low-privilege, reserve logs/system settings for trusted roles, require managed SSH key uploads under `storage/ssh-keys`, and never accept arbitrary private-key filesystem paths.

### Frontend And UI

- Problem: production frontend breaks or bloats after build changes. Trigger: minification, obfuscation, dependency imports, or repeated Vite builds. Rule: avoid stacking aggressive minification with obfuscation, import only needed ECharts modules, and remove stale hashed assets not referenced by `public/index.html`.
- Problem: admin pages become slow or visually unstable as data grows. Trigger: dashboard/list endpoints, user rows, setup/login layouts, sidebar branding, or desktop shell changes. Rule: keep `panel/state` lightweight, paginate multi-record views on the backend, keep desktop shell at `100vh` with explicit scroll containers, and treat responsive grid tracks as one unit.
- Problem: mock or realtime UI state behaves inconsistently. Trigger: mock data factories, mock settings, realtime log/audit refresh. Rule: initialize shared mock state before factories, store mock controls in `system_settings`, and use Vue `nextTick` with container refs when auto-scrolling refreshed logs/audit records.

### Nodes, Agent, And Hysteria Runtime

- Problem: node creation leaves broken state or fails during first Agent install. Trigger: save-then-install flows or using Agent tasks before registration. Rule: use backend create-and-install with rollback on failure; during first install, restart Hysteria over SSH before bootstrapping/registering Agent.
- Problem: remote execution fails because secrets, dependencies, or command shape are wrong. Trigger: SSH password use, Agent bootstrap, minimal Linux hosts, or large embedded binaries. Rule: decrypt node secrets immediately after loading records, preflight/install required tools such as `curl` and `openssl`, detect remote architecture, and use signed Agent download URLs instead of huge SSH commands.
- Problem: Agent/Hysteria communication, auth, or realtime metrics fail in production. Trigger: heartbeat, task payloads, local auth, Hysteria HTTP auth, panel outages, public API URL changes, or user traffic rate views. Rule: heartbeat updates latest metrics and cached user traffic, default realtime cadence is 2s, history comes from explicit snapshots, realtime views read cache and preserve last visible rates across page switches instead of creating polling tasks, task payloads are objects, Hysteria auth supports POST, Agent survives panel outages, and local Agent auth controls new connections when the panel is unreachable.
- Problem: logs are noisy, missing, or overload SSH nodes. Trigger: realtime log polling or systemd logging changes. Rule: enable high-frequency logs only for Agent-managed nodes, write Agent/Hysteria logs under `/var/log/mxinhy-agent/`, use timestamp/level prefixes, and prefer portable shell redirection in systemd units.

### Release, CI, And Upgrade

- Problem: CI/release automation fails from environment assumptions. Trigger: GitLab runner identity/known_hosts, dependency install mode, scp/ssh flags, Alpine tools, or version injection. Rule: configure git identity and host keys explicitly, keep build dev dependencies available, validate tool-specific flags, install required CI tools, and inject `VITE_APP_VERSION` from `package.json`.
- Problem: release and deploy flows duplicate work or publish the wrong artifact. Trigger: `main` pushes, local release scripts, tag pipelines, validation tags, or admin upgrades. Rule: let `build.sh` own version bump/commit/tag, keep CI focused on build/deploy, support skip-production validation tags, publish a stable runtime package, and make upgrades consume that package instead of source archives.
- Problem: release switches serve old or missing assets/binaries. Trigger: rolling `releases/current/shared` deployments, static dir persistence, cleanup ordering, or hot replacement from macOS. Rule: prune releases by modification time, never delete the active `current` target, keep static dir relative with embedded fallback for tag builds, build server binaries for Linux explicitly, and verify `ExecStart` plus file type before restarting systemd.

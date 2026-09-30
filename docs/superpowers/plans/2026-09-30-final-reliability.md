# Final Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Fix the confirmed reliability defects, verify CPU utilization against its source, and ship one tested formal release.

**Architecture:** Preserve existing collectors, management UI and transactional recovery. Separate persisted preferences from availability, reject stale writes, and retain component-level freshness when reusing data. Independent work packages have explicit file ownership; the coordinator integrates overlapping runtime/model changes and verifies the whole branch.

**Tech Stack:** Go, vanilla JavaScript, Playwright, C#/.NET Windows Forms, Docker/TrueNAS custom applications.

**Spec:** `docs/superpowers/specs/2026-09-30-final-reliability-design.md`.

## Global Constraints

- 保留现有排版、行顺序规则及用户配置；不增加常驻服务。
- 不提高现有采集频率；缓存复用只能减少重复请求，不能用旧数据伪造新的成功时间。
- 天气刷新周期保持：当前天气 10 分钟、分钟降雨 10 分钟、官方预警 5 分钟、日预报 1 小时。
- 不修改 Home Assistant、Plex、旁路由或 NAS 的其他服务；发布时只更新 Wallboard。
- 不返回、记录或提交密码、令牌、Cookie、密钥；已保存的秘密只在服务端使用。
- 保留备份加密、恢复白名单、体积限制、重新认证、事务日志和回滚机制。
- 兼容已有配置；接口增加字段时保留原字段语义，拒绝陈旧写入时给出可恢复的明确提示。
- Windows 实机表现与可在 Ubuntu 运行的核心逻辑测试分开报告，不把后者当成前者。

## Review Focus

- Stale tabs and unsaved edits during an integration mutation must not erase other changes.
- An all-null graph must be unavailable, while actual zero CPU/network values remain valid.
- A successful weather component must not renew another component's failed/expired warning.
- Crashed atomic writes, highly compressed files and concurrent setup must not produce unrestorable backups or orphaned setup.
- Saved right-edge positions, removed monitors and missing CSS size messages must remain usable without overwriting intended placement.

## Verification Environment

Go: `/home/shudaizi/nas/.toolchains/go/bin/go`, `GOCACHE=/tmp/nas-wallboard-go-cache`.
JavaScript: prepend `/home/shudaizi/nas/.toolchains/node/bin` to PATH.
.NET: `/home/shudaizi/nas/.toolchains/dotnet/dotnet`.
Go httptest/Playwright tests need permission to bind temporary local ports. No test targets production mutation endpoints.

## Task 1: Settings and retained credentials

**Files:** Modify `internal/widget/service.go`, `internal/integration/service.go`, `internal/httpapi/layout.go`, `internal/httpapi/integrations.go`, `web/manage-api.js`, `web/manage.js`, `web/integrations.js`, `web/layout.js`; corresponding Go/JS tests and focused browser regression tests.

**Interfaces:** `widget.Service.Layout() Layout` exposes stored widget enabled preferences; availability remains separate. Add a layout revision token on management GET/PUT, compare atomically on save. `integration.Service.TestCandidate` accepts optional current instance identity through the probe request, resolving masks server-side. Integration center emits change notifications consumed by layout editor synchronization.

- [ ] Write tests for disable/enable preference retention, added/deleted sources with a stale editor, cross-tab stale saves, pending-save controls, saved-mask probe, unknown ID/type mismatch, and independent recovery-page initialization.
- [ ] Run tests; expected: the new defect reproductions fail against v1.0.5.
- [ ] Implement the interfaces above with unchanged read-only dashboard layout, explicit conflict errors and preserved drafts; no secret returned to the client.
- [ ] Run `go test ./internal/widget ./internal/integration ./internal/httpapi` and `npm test`; expected: all pass.
- [ ] Commit only owned files with `fix: preserve layout preferences and saved integration probes`.

## Task 2: Collection correctness and CPU check

**Files:** Modify `internal/truenas/collectors.go` and its tests, `internal/integration/manager.go` and its tests; coordinator integrates changes in the TrueNAS branch of `internal/integrations/runtime.go`. Add stale UI handling in `web/view.js`, `web/styles.css`, related JS/dashboard tests via coordinator.

**Interfaces:** Existing `CollectRealtime`, `CollectSystem`, `CollectMemory`, `CollectAlerts`, `CollectDiskHealth` signatures remain. Reuse successful source reads with bounded freshness; report failure if required samples have no valid value. Manager stops previous collector before starting replacement. Production source inspection uses read-only TrueNAS RPCs and `/api/status`, never secrets in output.

- [ ] Write tests with CPU 0 and 42.5, terminal null/older valid samples, all-null CPU/network/memory, bounded cache expiry and upstream call counts, unfiltered SMART despite dismissed alerts, and old collector completion after replacement.
- [ ] Run tests; expected: null/caching/handover reproductions fail.
- [ ] Implement sample validation and shared source reuse; do not substitute memory estimates that change semantics. CPU is total CPU percentage from TrueNAS Netdata, not per-core sum or frontend recomputation.
- [ ] Run `go test ./internal/truenas ./internal/integration ./internal/integrations`; expected: all pass, call counts do not rise.
- [ ] Compare source CPU timestamps/values with the Wallboard result and short read-only OS `/proc/stat` samples. Different sampling windows must be described, not forced to match.
- [ ] Commit owned files with `fix: validate metrics and reuse successful NAS source reads`.

## Task 3: Recovery and authentication

**Files:** Modify `internal/support/bundle.go`, `internal/support/restore.go`, `internal/httpapi/setup.go`, `internal/httpapi/settings.go`, `internal/httpapi/server.go` and corresponding tests. Do not edit layout/probe handlers owned by Task 1.

**Interfaces:** Existing backup/restore functions remain; share file allowlist and count/size validation. Setup uses an exclusive transaction boundary with state recheck after probing. All current-password verification handlers use the same `auth.ScopeReauth` budget.

- [ ] Write tests for root/nested atomic temporary files, total bytes/file count limits, exported-valid-data restore readiness, malicious archives, setup concurrency/journal ownership, and alternating password/username/reauth attempts.
- [ ] Run tests; expected: current temporary-file export, setup race and reauth bypass reproductions fail.
- [ ] Implement shared limits (2048 files, 2 MiB/file, 16 MiB total and ZIP), serialized setup commit, shared 5 attempts/10 minutes limit; preserve transactional restore and secret verification.
- [ ] Run `go test ./internal/support ./internal/httpapi ./internal/auth`; expected: all pass, existing recovery tests remain green.
- [ ] Commit owned files with `fix: align backup limits and serialize sensitive setup changes`.

## Task 4: Weather component freshness and units

**Files:** Modify `internal/weather/client.go`, `internal/model/status.go`, `internal/state/store.go`, `internal/dashboard/builder.go`, coordinator's qweather branch in `internal/integrations/runtime.go`; tests in each affected package.

**Interfaces:** `Client.Refresh(context.Context) (model.WeatherStatus, error)` publishes independently successful components with their timestamps/error metadata. Store retains partial weather results without advancing failed component freshness. Internal units remain metric; dashboard converts temperature-only display for `Units == "imperial"`.

- [ ] Write tests for each component failing alone, total initial failure, partial recovery, unchanged cached tick timestamps, expired rain/alerts, and metric versus imperial hazard-equivalent displays.
- [ ] Run focused tests; expected: existing all-or-nothing behavior fails partial refresh and initial placeholder assertions.
- [ ] Implement isolated component updates and bounded scheduling; no component request frequency increases. Expired components are unavailable, not assumed clear.
- [ ] Run `go test ./internal/weather ./internal/state ./internal/dashboard ./internal/integrations`; expected: all pass.
- [ ] Commit owned files with `fix: preserve weather partial results and component freshness`.

## Task 5: Desktop positioning, stale display and integrated release

**Files:** Modify `windows/NASWallboard.Desktop/DesktopForm.cs`, `windows/NASWallboard.Desktop.Core/WindowPlacement.cs`, core tests; coordinator owns `web/view.js`, `web/styles.css` and corresponding stale-state tests. Update release documentation with verified results.

**Interfaces:** Introduce initial-placement logic using saved coordinates and actual CSS size; do not persist provisional dimensions. Continue existing `WindowPlacement.Resize` anchoring after initialization. Dashboard header gains compact freshness state using existing metadata, no new requests.

- [ ] Write tests for bootstrap 420-wide window followed by actual 360-wide window preserving a 24px right gap, no saved coordinates, unavailable dimensions, removed monitor and stale-browser recovery.
- [ ] Run tests; expected: original bootstrap clamping loses intended margin and fails.
- [ ] Implement deferred actual placement and stale-header recovery without changes to row spacing/order.
- [ ] Run .NET core tests, `go test ./... -count=1`, `go vet ./...`, `npm test`, Playwright, Windows build/race checks in CI; expected: all green. Report any unavailable real Windows GUI check separately.
- [ ] Dispatch one fresh whole-branch reviewer with this plan/spec, CPU evidence, call-count tests and git diff. Fix important findings with regression tests before release.
- [ ] Create and attach PR, preserve immutable tags, wait for CI, publish next verified formal version. Back up Wallboard config/data, deploy digest only, verify health/ready/modules/config and HA accessibility, retain rollback backup.
- [ ] Commit final integration/tests/docs with `fix: stabilize desktop placement and stale data visibility`.

## Execution Ledger

User instruction on 2026-09-30: “我看不懂，你直接实施，顺便看一下那个处理器使用率准不准”. This explicitly waives further document approval gates; implementation proceeds without additional design questions.
Parallel-domain skill is used for Tasks 1–3 with disjoint ownership; coordinator executes Tasks 4–5 and integrates shared files. No task-specific reviewer loops; one fresh whole-branch review before release.
Baseline: linked worktree verified on `fix/final-reliability`; initial source is v1.0.5 plus the design document. Record RED/GREEN evidence and completed commits below as work progresses.

### Progress

- Task 1: implemented in `1491b5a`, `9b7fa34`; failures reproduced before fixes. Focused Go suites/vet green, JavaScript 60/60, settings browser 5/5 (including 409 draft recovery and source deletion).
- Task 2: implemented in `b247a87`; null/required metric, shared reads, failure cooldown and collector handover regressions observed RED→GREEN. Full Go suite green at that checkpoint; coordinator integrated source timestamp Store setters and runtime calls.
- Task 3: implemented in `12ae6d6`; residue, archive limits, concurrent setup and shared credential budget RED→GREEN; scoped and full Go suites green.
- Task 4: implemented in `b329d29`, `0ad48d5`; component failures, initial failure, partial publication, expiry, imperial display and preserved official-warning regressions RED→GREEN. Targeted weather/state/dashboard green.
- Task 5: desktop and stale UI implementation in `b329d29`; .NET 36/36 green and Windows client builds with no warnings/errors. Browser 24/26 green locally: only 1080p/1440p height assertions fail with the local font bundle (370.14/372.38 versus 370), requiring canonical CI verification; assertions unchanged.
- CPU evidence: installed TrueNAS25.10.7 reporting and realtime both read `truenas_cpu_usage.cpu`; middleware metrics utility reads `/proc/stat`, subtracts idle/iowait and sums all counters. Software33% matched its raw source sample. Same-window OS formula10.28% versus TrueNAS13.18%, with guest duplicate fraction3.34%.
- Decision: preserve the TrueNAS total CPU source and document its guest-time accounting bias. The additional CPU request is an accuracy diagnosis, not authorization to alter NAS middleware; changing upstream or adding another collector would expand scope and query burden.
- Final review, canonical CI, immutable release, backup/deployment and post-deployment checks remain pending.

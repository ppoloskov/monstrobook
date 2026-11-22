# The Monstrobook Journey: Rewriting Audiobookshelf in Go (Without Losing Our Minds)

*From OOM-killed Node pods and 532,000 lines of test bureaucracy to a 36K-LOC Go server with 0-finding differential parity against TypeScript v2.37.2.*

---

## TL;DR

Between **October 29 and November 20, 2026** (375 commits), we replaced the official TypeScript Audiobookshelf server (`v2.37.2`) with **Monstrobook**, a standalone Go server that speaks the exact same SQLite schema and client protocols (web SPA, official iOS/Android, and third-party apps).

Along the way, we built a working Go port, watched it pull 2.2 GiB of page cache on hourly scans, fixed that, got bogged down in a 44K-LOC `map[string]any` codebase policed by AST ratchets, threw out the internals in a **4-wave greenfield rewrite**, accidentally built a **532,000-LOC verification bureaucracy**, deleted all 536,887 lines of it in a single commit, and replaced it with a **Twin-Server Differential Oracle** that drove **768 live Go-vs-TS divergences down to 0**.

This document replaces the old notes in `archive/`. It captures what we tried, what worked, what blew up, and the hard-won lessons both humans and coding agents need to keep in mind.

---

## Act I: Why We Started — Escaping the OOM Killer (Oct 29 – Nov 3)

The project didn't start as an academic exercise. It started because our production Audiobookshelf pod (`media/abooks` on `hyperwombat.au.poloskov.net`) kept getting OOM-killed:

| Image | Memory Limit | Restarts | Working Set (p50 / max) |
| --- | ---: | ---: | --- |
| TS `2.36.0` | 1 GiB | **13** (OOMKilled) | 136.1 MiB / 1,022.8 MiB |
| TS `2.37.2` | 2 GiB | **11** (OOMKilled) | 113.4 MiB / 305.7 MiB |
| TS `2.37.2` | 4 GiB | 0 | 88.4 MiB / **2,163.8 MiB** |

Bumping the limit to 4 GiB stopped the crashes, but a single-user/household audiobook server spiking past **2.1 GiB** of working set felt absurd. We wanted a drop-in Go binary that could:
1. Open an existing `/config` and `/metadata` directory unchanged.
2. Let us roll back to upstream TypeScript `v2.37.2` at any time without corrupting SQLite.
3. Serve the stock upstream web UI (`/app/client/dist`) and existing mobile apps unmodified.

So we started porting (`718cc0a0`, literally titled *"messy beginning"*).

---

## Act II: The First Go Build — Fast RSS, Slow Scanner, and the Ratchet Trap (Nov 3 – Nov 11)

On **November 3**, we rolled out the first Go build (`sha-4616244`) to the cluster. It never crashed, and its process RSS dropped to **29.5 MiB** median. Victory? Not quite.

When we pulled 30 days of VictoriaMetrics telemetry (`ABOOKS-RESOURCE-REPORT-2026-11-10.md`), we found a surprise:
- **TypeScript 2.37.2** ran its hourly library scan in **~1.1 seconds**.
- **Go `sha-4616244`** took **20–32 seconds** per hourly scan and pulled **2.2 GiB of Linux page cache** into the cgroup!

Once we fixed the scanner's I/O behavior in `sha-b42969b` (**Nov 10**), the numbers flipped dramatically:

| Metric | TS `2.37.2` (4 GiB) | Go `sha-4616244` | Go `sha-b42969b` |
| --- | ---: | ---: | ---: |
| Hourly scan | ~1.1 s | 20–32 s | **0–1 s** |
| CPU (p50) | 0.29 mCPU | 0.15 mCPU | **0.07 mCPU** |
| RSS (p50) | 68.4 MiB | 29.5 MiB | **21.0 MiB** |
| Working Set (p50 / max) | 88.4 / 2,163.8 MiB | 68.4 / 635.7 MiB | **61.6 / 103.6 MiB** |
| Page Cache (p50) | 142.7 MiB | 2,217.5 MiB | **44.5 MiB** |

Peak working set dropped to **~5% of TypeScript's peak**.

### Meanwhile, in the codebase…

While the production numbers looked great, the Go codebase itself was becoming a maintenance nightmare. By November 6–10, it had grown to **~44.6K handwritten LOC** across **57 packages**:
- **Dynamic maps everywhere:** `1,003` uses of `map[string]any` and `326` `[]any` slices. Everything was marshaled through generic maps to emulate JavaScript objects.
- **Subtle parity bugs:** Swapped `(limit, page)` return tuples in series pagination; `scanner.QuickMatchPodcastEpisodes` wired in one direction while the fetcher was only ever set in a test; `podcasts.Handler` never closed on backup restore (leaking cron schedulers pinned to closed `*sql.DB` handles); `DATETIME` written in `RFC3339Nano` in two packages and Sequelize format in twelve others.
- **The `sqlc` illusion:** We tried migrating queries to `sqlc` hoping to slash boilerplate. In `L5` (users/sessions) and `L6` (progress/playback), `sqlc` saved **58** and **4** handwritten lines respectively. Why? Because `sqlc` couldn't eliminate the real bloat: converting between database rows, dynamic maps, and API DTOs.
- **Policing tools over architecture:** To stop the bleeding, we added an AST complexity ratchet, `depguard` layer rules, and `dbnest` checks. Net result: line count went *up* while agents spent half their turns fighting the ratchet.
- **The withdrawn `D02` cut:** Desperate to hit an arbitrary "30% LOC reduction" target, an early proposal tried to delete backups, M4B encoding, and metadata providers. It was immediately withdrawn—the whole point was a *drop-in* replacement, not a gutted server.

---

## Act III: The Hard Reset — "Only Two Contracts Are Fixed" (Nov 11 – Nov 12)

On **November 11**, we stepped back and made the single most important architectural decision of the project:

> **Only two contracts are fixed:**
> 1. **The SQLite database** (and `/config` + `/metadata` trees) — upstream v2.37.2 must open what Go wrote, and Go must open what upstream wrote.
> 2. **The real-client protocol** — the routes, JSON fields/types, auth flows, and Socket.IO events that the web SPA and mobile apps actually use.
>
> **Everything in between is ours to design.** We do not reproduce JavaScript `Number()` coercion, Express error pages, Sequelize `changed()` tracking, or JS truthiness for their own sake.

With that clarity, we froze the old 57-package tree (`7051b3e5`) and executed a **4-wave greenfield rewrite** (`7bfefa02` → `8990c2e6`):

### What Changed in the Rewrite
1. **One data mechanism (`internal/db`):** Replaced `sqlc`, `sqlupdate`, and 7× bespoke `rows.Scan` frameworks with `sqlx` + typed row structs for all 26 tables, plus dedicated column types (`db.Time` for Sequelize `DATETIME`, `db.JSON[T]` that preserves undecodable text without crashing, `db.Bool` for nullable `0/1`).
2. **Transactions in `context.Context`:** `db.Tx(ctx, fn)` attaches the transaction to `ctx`. Every `db` call inside `fn` automatically joins it. With SQLite's single-writer connection, this makes "query the pool inside a transaction" deadlocks structurally impossible.
3. **Compiler-enforced DAG:** Replaced `depguard` and AST ratchets with **32 domain packages** in a strict import hierarchy. If a lower package needs to notify a higher one, it takes a callback field in `Deps`.
4. **Typed in, typed out:** Request bodies decode into typed structs via `httpx.Decode[T]` (a wrong JSON type is a clean `400`). PATCH bodies use pointer fields and null-recording types only where clients actually send `null` to clear a field.
5. **In-repo Engine.IO v4 / Socket.IO v4 (`internal/realtime`):** The external Socket.IO library had a connection/authentication race (51/200 immediate-auth connections failed). Writing a focused WebSocket-only Engine.IO/Socket.IO transport in Go fixed the registration ordering and made per-user event filtering trivial. *(Tradeoff: long-polling transport is unsupported; all modern ABS web and mobile clients use WebSockets.)*
6. **`log/slog` everywhere:** Replaced custom multi-sink printf loggers with a standard `slog.Handler` feeding console, daily files, ring buffer, and Socket.IO log listeners.

The rewrite landed in ~48 hours: **35,863 production LOC** (zero generated code, zero `TODO`/`FIXME`s), down from 39.7K + 3.2K generated.

---

## Act IV: The 532,000-LOC Test Machine Detour (Nov 13 – Nov 19)

After the rewrite, we needed to prove parity. And here we fell into a classic engineering trap: **we over-engineered the verification harness.**

We built `testmachine/`: a declarative register of **345 requirements**, **647 invariants**, **34 owner packets**, **2,041 scenarios**, citation linters, fixture-overfit audits, ledger validators, and formal review-receipt JSON files. By November 19:
- The test harness had ballooned to **~532,000 lines of code and fixtures**—nearly **15× larger** than the Go server it was testing.
- Agents burned through weekly and session quotas (`opencode-go`, `ollama-cloud`) writing and reconciling scenario bindings and review receipts.
- Only **7 of 34** packet review receipts were accepted. Worse, all those hand-authored expectations were still *our interpretation* of what TypeScript did, not TypeScript itself.

The realization hit on **November 19–20**: *Why are we maintaining half a million lines of static expectations when we can just boot the real TypeScript v2.37.2 server next to Go and diff every byte they produce?*

---

## Act V: The Twin-Server Differential Oracle & Road to Zero (Nov 20)

In commit `9341b95f`, we executed the cleanest deletion in the repo's history:

```text
1178 files changed, 13961 insertions(+), 536887 deletions(-)
```

We replaced the entire 532k-LOC harness with the **Twin-Server Differential Oracle** (`testmachine/oracle/`).

### How the Oracle Works
`make verify` (now backed by a reproducible `flake.nix` + `flake.lock` with Go, Node 22, and WebP-enabled FFmpeg) boots **pinned TS v2.37.2** (`NODE_V8_COVERAGE=v8/`) and **Go (`monstrobook`)** (`GOCOVERDIR=gocover/`) side-by-side on isolated copies of a seeded installation. Every step runs against both servers simultaneously and diffs:
- **HTTP:** status, headers, cookies, strict JSON types, JWT claims.
- **SQLite:** all **26 tables** AND `typeof()` storage classes (catching Go writing `TEXT` `""` where TS writes `NULL`, or integer vs. real mismatches).
- **Socket.IO:** 3 simultaneous audiences (`root`, `listener`, `finisher`).
- **Filesystem:** directory trees and normalized `metadata.json`.
- **Outbound stubs:** HTTP (metadata providers/covers) and SMTP (email delivery).
- **Bidirectional rollback:** `rollback_go_to_ts` and `rollback_ts_to_go` steps that stop one server, hand its `/config` + `/metadata` to the other, and verify clean boot and identical API responses.

### Driving 768 Findings to 0 (Waves P1–P5)
On its first full run, the oracle immediately exposed **768 unexpected differences** across 205 groups—things our old hand-written tests never caught. We burned them down in five focused waves:
1. **Wave P1 (`768 → 349`):** Split historical vs. admin-enriched playback session DTOs (`HistorySession` vs. `AdminHistorySession`), fixed `deviceDescription` (`"null null / null"`), and fixed `browse` secondary sort tie-breakers.
2. **Wave P2 (`349 → 220`):** Unified `media.ProbeAudio` (37 metadata tags), stopped bad-JSON library scans from launching background jobs, extracted embedded cover art on scan, and fixed `quick-match` `titleIgnorePrefix`.
3. **Wave P3 (`220 → 131`):** Added global `validateJSONBody` edge middleware (`10 MiB` limit matching `express.json()`), which collapsed Route Matrix failures from **55 → 1**; deduplicated `scan/audio.go` onto `media.ProbeAudio`.
4. **Wave P4 (`131 → 47`):** Preserved `autoDownloadSchedule: ""` as SQLite empty text, added deterministic SQLite post-`ANALYZE` tie-breakers, separated `share` `playbackSession.id` UUIDs from cookies, and hit **654 / 654 Route Matrix PASS**.
5. **Wave P5 (`47 → 0`):** Fixed a real SQL bug querying a nonexistent `slug` column in `customMetadataProviders`, added `db.AudioIndex` (marshals `0` as `null` for scanned podcast episodes), paired podcast episodes on rescan by path/inode, and added a `2s` `watcherRenameTimeout` matching `Watcher.js`.
6. **Wave S1 (`54997df5`):** Structural compression across `metadata`, `item`, `list`, and `logging` (`-273 prod LOC`, `-7 files`), confirming our **~36.1K prod LOC** is **2.6× smaller** than upstream TS (`92.7K` LOC) without resorting to code golf.

### The Scorecard at `54997df5`
- **Go Parity Backlog:** **`0` unexpected findings** (`make verify` exits `0`)
- **Flow Steps:** `377 / 377` PASS | **Complex Tests:** `17 / 17` PASS | **Route Matrix:** `654 / 654` PASS
- **Routes Hit:** `218 / 218` (**100%**) in-scope routes (`217/218 = 99.5%` happy-path, `214/218 = 98.2%` error-probed)
- **Web Client Audit:** `171 / 171` (**100%**) `$axios` routes and `6 / 6` Socket.IO emitters exercised
- **Coverage:** **80.3%** TS V8 in-scope lines (`32,730 / 40,752`); **71.7%** Go statement coverage; `golangci-lint`: **0 issues**
- **OpenAPI:** Live 3.0.3 spec generated from oracle traffic (`docs/openapi.json`: 188 paths, 224/224 operations)

---

## Key Decisions, Tradeoffs & Intentional Divergences

Where Go intentionally differs from upstream TypeScript (recorded in `docs/CHANGES.md` and `testmachine/oracle/divergences.json`):

1. **Security hardenings over bug-for-bug parity:**
   - Custom metadata providers and cover downloads go through an SSRF guard.
   - Internal errors (SMTP replies, DB errors, host paths) are logged server-side and never leaked to non-admin clients.
   - Socket.IO events are strictly filtered per user's `Principal` (upstream's `SocketAuthority` leaks `episode_*` events to listeners without podcast access).
   - Revoked sockets receive nothing; `online` user events omit tokens and permission maps.
2. **Process & lifecycle hygiene:**
   - Go reaps exited/canceled FFmpeg processes promptly (1s `exec.Cmd.WaitDelay` for pipe draining) instead of upstream's 10s retry wait or leaving orphaned FFmpeg children on shutdown.
   - Bad library/backup cron expressions are logged and skipped instead of crashing server boot.
3. **Strict typed JSON over JS coercion:**
   - Wrong JSON field types return `400` rather than silently coercing via `Number()` (`"" → 0`, `true → 1`, `[5] → 5`).
   - CORS middleware is ported so mobile cover/ebook loads work; error bodies are plain text.
4. **Deferred upstream behaviors (defaults: NO):**
   - We do not run `ANALYZE` at boot or port upstream's boot-time `cleanDatabase` unless proven necessary.

---

## Essential Lessons for Humans & Agents

If you are working on Monstrobook (or building a similar replacement), burn these into memory:

### Technical Gotchas
1. **SQLite `typeof()` matters:** Go's `""` vs `nil` (`NULL`) and `0` vs `nil` matter to Sequelize on rollback. Use `db.Time`, `db.Bool`, `db.JSON[T]`, `db.AudioIndex`, and pointer fields deliberately.
2. **Upstream prunes short sessions on boot:** Mobile offline-session payloads in reopen/rollback tests must have **> 3 seconds** of listening, or TS boot will prune the row.
3. **Never share mutable `/config` or `/metadata`:** TS and Go must always run on independent copies; re-stat copied media fixtures because inodes change across copies.
4. **WebP FFmpeg is required:** Cover and author-image tests return `500` if `ffmpeg` lacks `libwebp`. Use `nix develop` (recommended) or the FFmpeg prefix in `TESTING.md`.
5. **Localhost `::1` & loopback:** Some sandboxed test environments block `[::1]:0` binding or loopback HTTP; don't mistake a sandbox bind failure for a logic bug.

### Engineering & Process Lessons
1. **Diff against reality, not your own specs:** Hand-written parity tests test what you *think* upstream does. A live differential oracle against the pinned upstream binary tests what it *actually* does—and deletes 500k lines of maintenance burden.
2. **Line-count targets are a trap; architectural boundaries are the lever:** Trying to shave 30% off a messy codebase with `sqlc` and AST ratchets failed. Fixing the contracts and rewriting cleanly cut complexity naturally and got us to 2.6× smaller than upstream.
3. **Agent economics & workflow:**
   - Run workers on capable mid-tier models (Sonnet / medium effort) with **disjoint, bounded file ownership**—at most 3–4 in parallel.
   - Never run an owner/reviewer loop on every packet; reserve review passes for failures or high-risk packets.
   - Cluster failures by root cause before touching code (e.g., one `validateJSONBody` edge middleware closed 54 route-matrix failures at once).
4. **Verify before declaring parity:** A passing `Route Matrix` or structural shape check does not prove security or concurrency safety. Even at **0 oracle findings**, our pre-release audit (`docs/RELEASE_CHECKLIST.md`) uncovered real P0/P1 issues (IDORs, ZIP-bomb bounds, idle HLS/share session caps, graceful `WaitGroup` drain) that only dedicated security & state-machine review catches.

---

## Where to Go From Here

- **Active state & next steps:** [handover.md](../handover.md) and [docs/RELEASE_CHECKLIST.md](../docs/RELEASE_CHECKLIST.md) (Gates 1–2 complete; currently **Gates 3–4: Release Verification & Tagging** for `v0.1.0`)
- **Architecture & package DAG:** [ARCHITECTURE.md](../ARCHITECTURE.md)
- **Oracle & mobile recording (`cmd/abs-record`):** [testmachine/oracle/README.md](../testmachine/oracle/README.md)
- **Deliberate differences from pre-rewrite Go:** [docs/CHANGES.md](../docs/CHANGES.md)

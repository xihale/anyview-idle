# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Go backend (HTTP + WebSocket client, `go:embed`), frontend is Vue 3 + Element Plus built with
Vite (`web-src/`, output embedded from `web/dist`). User directive (2026-09-20) replaced the
earlier "no framework" constraint with "用 Vue，尽量完善的方案（如 Element 库）"; the teal
status-lite design language is applied by theming Element's CSS variables. Single-binary
deployment unchanged.

## Users

A small circle of classmates sharing one deployment, each with their own AnyView account. Each browser holds an independent cookie session: its own login, its own run, its own status — nobody sees anybody else's account or progress. The operator deploys it on their router behind a reverse proxy; sessions run minutes to hours, mostly unattended; users glance at it and interact only to configure, start, and stop.

## Product Purpose

anyview-idle quietly works on the AnyView OJ platform in two modes. **Idle** accrues "accumTime" (recorded study time): it holds exactly one timing session at a time (exam WebSocket 401 message pairs) per unpassed question until that question's accumulated time reaches a randomly drawn target inside a user-set range; after each hold it can inject a controlled number of *error* attempts (compile errors or failing runs using the student's own unpassed code) to make the account's activity record look human. **Solve** actually passes questions automatically via the verified oracle-leak method (analysis/09 appx.2): submit the untouched template as a stub, read the per-group input/expected pairs leaked through the judge echo, submit generated table-lookup code, and restore the archived student code byte-identical afterwards. Success means: time accrues server-side / questions flip to passed, and the operator does nothing.

## Positioning

A machine whose whole job is to sit perfectly still: it measures patience, not speed. It is the anti-productivity tool — the UI's work is waiting, so the interface's character is stillness. No neighboring tool for this platform ships a calm idle-farmer; they ship batch scripts.

## Operating Context

- Binds `127.0.0.1` and is exposed through a reverse proxy (Caddy). The API has no user-facing auth: sessions are cookie-isolated (128-bit random sid), so anyone reachable can create their own session and brush — gate at the proxy if that is not wanted.
- All accounts share one egress IP and one upstream rate limit (`-rpm`, default 20/min, sliding 60s window): parallel brushing queues politely instead of tripping the WAF.
- Used next to a terminal, often at night, often for long stretches; glanceability matters more than density.
- Ritual: login (or paste a token) → pick course → set time range + error range → start → walk away → come back to a summary.
- Platform realities: WAF pacing (tens of requests/minute), token ~12h lifetime, server times only one question at a time (serial holds).

## Capabilities and Constraints

- Two task modes share one pipeline (catalog → filter unpassed → per-question work → summary),
  and the same persistence/auto-relogin machinery.
- Idle: time target is a range; each question draws a random target in it. accum ≥ range floor
  ⇒ skip (time never brushes downward). Passed questions are never brushed.
- Idle error injection happens **after** the hold, is capped by a range, never injects a run
  that would pass, and snapshots/restores the archived student code around every attempt.
  Lab questions (questionType ≥ 3) are skipped.
- Solve: stub-probe leaks the answer table through the judge echo; generated lookup code must
  actually pass runGroup before anything counts — no fake bookkeeping. The archive is restored
  (verified by read-back) after every attempt, passed or not. Probe-only mode never submits
  round-2 and never changes pass. Oracle formats the parser cannot read are dumped to the log
  and the question is marked failed, never guessed.
- Terminal output is a timestamped running log; the log is the audit trail and must stay readable.

## Brand Commitments

- Name: `anyview-idle` (lowercase, hyphenated).
- The operator's brief binds this surface: **flat, minimal, Zen, visually-first** (扁平化、极简、Zen 模式、视觉导向优先).

## Evidence on Hand

- Protocol verified live 2026-09 (RSA login, WS 401 timing pairs, compile/runGroup counters, accumTime behavior) — see analysis docs in the sibling repo `fuckView/analysis/` (private, not published).
- Real end-to-end runs succeeded: holds verified (+27s, +96s, +73s), cmpErrorCount accumulates server-side, archive restored byte-identical every time.
- No logo, no marketing copy, no imagery exists. Do not fabricate any.

## Product Principles

1. **Calm is the feature.** The UI must never nag, flash, or demand attention; a running session should look like stillness, not like progress-bars screaming.
2. **State at a glance from across the room.** One look answers: is it running, which question, how long until it moves on.
3. **Honesty over polish.** The log shows exactly what happened; numbers are real platform counters.
4. **Nothing moves that isn't working.** Motion is reserved for genuine activity; idle is visually idle.

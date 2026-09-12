# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Existing codebase: Go backend (HTTP + WebSocket client, `go:embed`), frontend is a single pure-HTML/vanilla-JS file (`web/index.html`) served by the binary. No build step, no framework. This is settled and not up for redesign.

## Users

One person: the operator themself — a technical university student running the console on `127.0.0.1` in a browser window parked next to a terminal. Sessions run minutes to hours, mostly unattended; the operator glances at it from across the desk and interacts only to configure, start, and stop.

## Product Purpose

anyview-idle quietly accrues "accumTime" (recorded study time) on the AnyView OJ platform. It holds exactly one timing session at a time (exam WebSocket 401 message pairs) per unpassed question until that question's accumulated time reaches a randomly drawn target inside a user-set range; questions already at or above the range floor are skipped. After each hold it can inject a controlled number of *error* attempts (compile errors or failing runs using the student's own unpassed code) to make the account's activity record look human. Success means: time accrues server-side and the operator does nothing.

## Positioning

A machine whose whole job is to sit perfectly still: it measures patience, not speed. It is the anti-productivity tool — the UI's work is waiting, so the interface's character is stillness. No neighboring tool for this platform ships a calm idle-farmer; they ship batch scripts.

## Operating Context

- Localhost only (`127.0.0.1` bind; the API has no auth — must never be exposed).
- Used next to a terminal, often at night, often for long stretches; glanceability matters more than density.
- Ritual: login (or paste a token) → pick course → set time range + error range → start → walk away → come back to a summary.
- Platform realities: WAF pacing (tens of requests/minute), token ~12h lifetime, server times only one question at a time (serial holds).

## Capabilities and Constraints

- Time target is a range; each question draws a random target in it. accum ≥ range floor ⇒ skip (time never brushes downward).
- Passed questions are never brushed; questions already in range are skipped.
- Error injection happens **after** the hold, is capped by a range, never injects a run that would pass, and snapshots/restores the archived student code around every attempt. Lab questions (questionType ≥ 3) are skipped.
- Terminal output is a timestamped running log; the log is the audit trail and must stay readable.

## Brand Commitments

- Name: `anyview-idle` (lowercase, hyphenated).
- The operator's brief binds this surface: **flat, minimal, Zen, visually-first** (扁平化、极简、Zen 模式、视觉导向优先).

## Evidence on Hand

- Protocol verified live 2026-09 (RSA login, WS 401 timing pairs, compile/runGroup counters, accumTime behavior) — see analysis docs in the sibling repo `fuckView/analysis/`.
- Real end-to-end runs succeeded: holds verified (+27s, +96s, +73s), cmpErrorCount accumulates server-side, archive restored byte-identical every time.
- No logo, no marketing copy, no imagery exists. Do not fabricate any.

## Product Principles

1. **Calm is the feature.** The UI must never nag, flash, or demand attention; a running session should look like stillness, not like progress-bars screaming.
2. **State at a glance from across the room.** One look answers: is it running, which question, how long until it moves on.
3. **Honesty over polish.** The log shows exactly what happened; numbers are real platform counters.
4. **Nothing moves that isn't working.** Motion is reserved for genuine activity; idle is visually idle.

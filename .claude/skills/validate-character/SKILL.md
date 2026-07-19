---
name: validate-character
description: Check whether an existing In Darkened Dreams (IDD) LARP character build is legal — verifies CP spend, attribute costs, header/skill prerequisites, faction gating, duplicate purchases, and repeatable limits, and reports every rule violation. Use when someone wants to verify, audit, or fix an IDD character sheet or build.
---

# In Darkened Dreams — Character Validator

Check an existing IDD character build against the rules and report every violation. The group uses **hard-block** rules: a build is either legal or it isn't, and you must clearly flag anything illegal.

## How to run it

The deterministic engine is `.claude/ids-data/validate.py`. Never hand-compute CP or judge prerequisites yourself — run the script; it is authoritative.

1. Get the build. If the player has a saved `*.character.json`, use it. If they describe it in prose or paste a spreadsheet, transcribe it into the build JSON format (below) first.
2. Run: `python3 .claude/ids-data/validate.py --sheet <build.json>`
   - Exit code 0 = legal; 2 = illegal.
   - It prints a character sheet (if legal) plus a report of ERRORs (hard rule breaks) and warns (things to check manually, e.g. prose prerequisites the engine couldn't auto-resolve).
3. Explain the result plainly. For each ERROR, name the rule and suggest a concrete fix (e.g. "you're 3 CP over — drop Buckler, or lower Fire from 3 to 2"). For each warning, tell the player what to eyeball.

## Build JSON format

```json
{
  "name": "Cornelius",
  "heritage": "Grelkyn",
  "faction": "The Veilward Enclave",
  "cp_sources": {"starting": 30, "history": 5, "bullets": 0, "donations": 0, "playtest": 0, "earned": 0},
  "attributes": {"Air": 2, "Earth": 2, "Fire": 2, "Water": 3, "Void": 3},
  "headers": ["Investigator"],
  "skills": [{"name": "Celerity"}, {"name": "Craftsman", "count": 2}]
}
```
`available_cp: <n>` may be used instead of `cp_sources` if only the total is known.

## What the validator checks (hard blocks)

- Total CP spent (attributes + headers + skills) ≤ available CP.
- Attributes ≥ 2; attribute CP cost computed as the sum of levels above 2.
- Heritage and faction are real; only one faction.
- Header faction-membership requirements and header exclusions (e.g. Penitent vs Cupbearer).
- Every purchased skill is reachable (open, chosen heritage, or an owned header/sphere).
- No duplicate purchase of a non-repeatable skill; repeatable purchase limits respected.
- Spells require ownership of their sphere.

## What it can't fully auto-check (surface as warnings)

- Prerequisites written as prose ("2 purchases of Armored for War", "Evasive **or** Mystic Armor" OR-semantics) are flagged for manual confirmation rather than silently passed or failed.
- Appearance/costuming requirements and roleplay obligations from heritage/faction — remind the player, but they're not mechanical CP checks.

Always end with a clear verdict: **LEGAL** (with the sheet) or **ILLEGAL** (with the numbered fixes).

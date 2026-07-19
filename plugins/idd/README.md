# In Darkened Dreams — Character Builder

An interactive character builder for the **In Darkened Dreams** (IDD) LARP,
built as a set of Claude Code skills backed by the ruleset (v1.0.3) extracted
into structured data plus a deterministic rules engine.

Sit down with Claude Code in this directory and it will guide you through
building a legal character — enforcing all the CP, attribute, header, skill,
faction, and spell rules — and hand you a validated character sheet.

## Quick start (for players)

You don't need to know any code to use this.

1. Install **Claude Code** (Anthropic's CLI) and get this folder onto your
   computer (clone the repo, or copy the folder your GM shared).
2. Open a terminal in this folder and run `claude`.
3. Type **`/build-character`** and answer the questions. That's it — at the end
   you get a finished, rules-legal character sheet.

You can also just talk to it: *"help me build a sword-and-board healer"* works
as well as the slash command. If you already have a character and earned CP,
use **`/level-up`**; to double-check a sheet, use **`/validate-character`**.

## Using it

Open Claude Code in this folder and just ask, or invoke a skill directly:

| You want to… | Say / run |
| --- | --- |
| Build a new character | `/build-character` — or "help me make an IDD character" |
| Check an existing build is legal | `/validate-character` |
| Spend newly-earned CP on a character | `/level-up` |

The builder is conversational: describe a concept ("a stealthy healer with a
bit of fire magic") and it translates that into legal mechanical choices,
explaining the tradeoffs and keeping a running CP tally.

## What's under the hood

```
.claude/
  skills/
    build-character/SKILL.md      interactive new-character flow
    validate-character/SKILL.md   audit an existing build
    level-up/SKILL.md             additive advancement flow
  ids-data/
    rules.json        CP budget, attribute cost curve, vitality formula, tiers
    heritages.json    6 heritages (skills, disadvantages, appearance reqs)
    factions.json     4 factions (each unlocks 3 exclusive headers)
    open-skills.json  12 open skills (no header required)
    headers-a/b/c.json   27 skill headers with costs, prereqs, and skills
    magic.json        rules for spheres, casting, spell types
    spells.json       49 spells across the 6 spheres
    validate.py       the deterministic rules engine (source of truth)
```

### The rules engine

`validate.py` does all CP math and legality checks — the skills call it rather
than eyeballing anything. Enforcement is **hard-block**: an illegal build fails.

```bash
python3 .claude/ids-data/validate.py --catalog          # list all options
python3 .claude/ids-data/validate.py --sheet build.json # validate + print sheet
python3 .claude/ids-data/validate.py build.json         # validate only (exit 0/2)
```

It checks: CP budget (attributes + headers + skills ≤ available), attribute
minimums and cost curve, `Vitality = ⌈(Earth+Void)/2⌉`, heritage/faction
validity, header faction-gating and exclusions, skill/header/heritage
reachability, sphere→spell gating, duplicate purchases, and repeatable limits.
It derives traits (heritage, faction, headers, Arcane/Devout, experience tier).

### Build file format

```json
{
  "name": "Cornelius Allegretti",
  "heritage": "Grelkyn",
  "faction": "The Veilward Enclave",
  "cp_sources": {"starting": 30, "history": 5, "bullets": 0, "donations": 0, "playtest": 0, "earned": 0},
  "attributes": {"Air": 2, "Earth": 2, "Fire": 2, "Water": 3, "Void": 3},
  "headers": ["Investigator"],
  "skills": [
    {"name": "Celerity"},
    {"name": "The Sphere of Conjuration"},
    {"name": "Crushing Bonds"},
    {"name": "Craftsman", "count": 2}
  ]
}
```

## Data provenance & verification

The ruleset data was extracted from *In Darkened Dreams Rules v1.0.3* and then
**cross-checked line-by-line against the source** — every one of the 27 headers,
371 header-skills, 49 spells, 12 open skills, 6 heritages, and 4 factions was
audited for missing entries, wrong CP costs, truncated descriptions, and bad
prerequisites. **Result: 0 discrepancies.** The engine was also validated
against a real play-group character (Cornelius, 35 CP), which it reproduces
exactly.

## Known limitations (v1)

- **Prose prerequisites** — a few skills gate on things the engine can't count
  automatically (e.g. "2 purchases of Armored for War", "any Melee Weapon
  skill"). These produce a **warning** to verify manually. Single-skill
  prerequisites and OR-prerequisites ("Evasive **or** Mystic Armor") *are* fully
  enforced as hard blocks.
- **Attribute activation costs** on skills/spells are recorded for reference but
  don't affect build legality (they're spent during play, refreshed each event).
  A few skills with alternate/secondary spends only record the primary cost;
  the full text is in each skill's description.
- **Costuming / roleplay obligations** from heritage and faction are surfaced as
  reminders, not mechanical checks.

If you spot a data error against the rulebook, the fix is almost always a small
edit to one of the JSON files in `.claude/ids-data/`. The full extracted source
text lives in `ids.txt` for easy cross-referencing.

Ruleset: *In Darkened Dreams Rules v1.0.3*.

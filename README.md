# In Darkened Dreams — Character Builder

An interactive **character builder**, **validator**, and **level-up** tool for the
*In Darkened Dreams* (IDD) LARP. It enforces every CP, attribute, header, skill,
faction, and spell rule against the v1.0.3 ruleset and hands you a finished,
rules-legal character sheet.

It installs two ways — pick whichever you use:

## ▶ Install in Claude Desktop (or claude.ai) — no Claude Code needed

This is the easiest route for most players. It works on **any plan, including
Free**, as long as code execution is turned on. You don't need to install Python.

1. **Download the skill ZIP:**
   **https://github.com/verveguy/idd/releases/latest/download/idd-character-builder.zip**
2. **Turn on code execution:** Claude **Settings → Capabilities → "Code execution
   and file creation" → ON**. (The builder runs its rules-checker in Claude's
   sandbox — this is what lets it do that.)
3. **Add the skill:** Claude **Settings → Skills → Add**, and select the ZIP you
   downloaded. Toggle it **ON**.
4. **Use it** — just chat:
   > "Help me build an In Darkened Dreams character — a sword-and-buckler spellblade."

Claude will walk you through heritage, faction, attributes, headers, skills, and
spells, and hand you a validated sheet. It also checks existing characters and
levels them up when you earn CP.

To update later, download the newest ZIP from the same link and re-add it.

## ▶ Install in Claude Code (plugin)

If you use [Claude Code](https://claude.com/claude-code), install it as a plugin
instead:

```
/plugin marketplace add verveguy/idd
/plugin install idd@idd-marketplace
```

Then use it anywhere:

```
/idd:build-character      build a new character, step by step
/idd:validate-character   check an existing character is legal
/idd:level-up             spend newly-earned CP on a character
```

You can also just talk to it — *"help me build a sword-and-board healer"* works as
well as the slash command. Update later with `/plugin marketplace update
idd-marketplace`.

## What's in the box

## ▶ Companion web builder (optional)

Prefer clicking to typing? There's a visual builder at
**https://v3rv.com/idd/dist/character-builder-app.html** — dropdowns for
heritage/faction, attribute steppers, header/skill pickers, a **live CP ledger +
validation**, and a printable sheet. It runs fully in your browser (no install).
Use **Copy build JSON** to paste a build into a chat with the skill, or **Import
JSON** to load one the skill made. It pairs with the skill; the skill's validator
is still the final word.

## What's in the box

Either way, it bundles the three skills **and** the full ruleset data, so it works
with no extra setup:

- **27 headers** (371 skills), **49 spells** across 6 spheres, **12 open
  skills**, **6 heritages**, **4 factions** — all extracted from the rulebook
  and cross-checked line-by-line against the source (**0 discrepancies**).
- A deterministic **validator** (`validate.py`) that does all CP math and
  legality checks — hard-blocking illegal builds — so nothing relies on
  guesswork. Verified against a real play-group build (Cornelius, 35 CP).

## Repository layout

```
.claude-plugin/marketplace.json   ← the marketplace (lists the plugin)
plugins/idd/                      ← the plugin itself
  .claude-plugin/plugin.json
  skills/                         ← build-character, validate-character, level-up
  ids-data/                       ← ruleset JSON + validate.py (travels with plugin)
  README.md                       ← full mechanics & data-model docs
characters/                       ← example validated builds
ids.txt                           ← extracted source text (for data corrections)
```

See [`plugins/idd/README.md`](plugins/idd/README.md) for the full mechanics
reference, build-file format, and data-provenance notes.

## Developing / testing locally

Point Claude Code at your working copy instead of GitHub:

```
/plugin marketplace add ./           # add this repo as a local marketplace
/plugin install idd@idd-marketplace
```

Validate the manifests after edits:

```
claude plugin validate ./plugins/idd
claude plugin validate .
```

If you fix a data error, edit the relevant file in `plugins/idd/ids-data/` (the
single source of truth) and **bump the `version`** so installed users receive the
update. To rebuild the Claude Desktop Skill ZIP from that master, run the build
script — it syncs the data, stamps the version everywhere, builds the zip, and
smoke-tests it:

```bash
./build.sh --version 1.0.3
```

See [`dist/README.md`](dist/README.md) for the full Desktop-Skill build/release
flow. The built `.zip` is git-ignored (a build artifact); distribute it via GitHub
Releases.

---

Ruleset: *In Darkened Dreams Rules v1.0.3*. This is an **unofficial, fan-made**
aid; the rulebook PDF is not distributed here. Game content is © its respective
creators — see [`NOTICE.md`](NOTICE.md).

## License

The **tool software** in this repo is © 2026 verveguy and released under the
[MIT License](LICENSE) — use it, fork it, adapt it freely. The MIT grant covers
the software only; the *In Darkened Dreams* game content and the transcribed
ruleset data are **not** MIT-licensed and remain © their respective creators
(see [`NOTICE.md`](NOTICE.md)).

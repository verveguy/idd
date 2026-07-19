# In Darkened Dreams — Claude Code Plugin

A Claude Code **plugin marketplace** for the *In Darkened Dreams* (IDD) LARP. It
installs an interactive **character builder**, **validator**, and **level-up**
tool that enforce every CP, attribute, header, skill, faction, and spell rule
against the v1.0.3 ruleset — and hand you a finished, rules-legal character
sheet.

## Install (players)

You need [Claude Code](https://claude.com/claude-code) installed. Then, in any
Claude Code session:

```
/plugin marketplace add verveguy/idd
/plugin install idd@idd-marketplace
```

That's it. Now use it anywhere:

```
/idd:build-character      build a new character, step by step
/idd:validate-character   check an existing character is legal
/idd:level-up             spend newly-earned CP on a character
```

You can also just talk to it — *"help me build a sword-and-board healer"* works
as well as the slash command. No coding required.

To update later when new versions ship:

```
/plugin marketplace update idd-marketplace
```

## What's in the box

The plugin bundles the three skills **and** the full ruleset data, so it works
offline with no extra setup:

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

If you fix a data error, edit the relevant file in `plugins/idd/ids-data/` and
**bump the `version`** in both `plugin.json` and `marketplace.json` so installed
users receive the update.

---

Ruleset: *In Darkened Dreams Rules v1.0.3*. This tool is a fan-made aid for the
play group; the rulebook PDF is not distributed here.

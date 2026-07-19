# In Darkened Dreams — Claude Desktop / claude.ai Skill

This folder builds **`idd-character-builder.zip`**, a custom **Agent Skill** you
can upload to the **Claude Desktop app** (or claude.ai in a browser) so you can
build IDD characters in normal chat — **no Claude Code required**.

It bundles the same verified ruleset data and Python validator as the Claude Code
plugin. The validator runs inside Claude's code-execution sandbox, so **you do
not need Python installed** on your computer.

## Requirements

- **Code execution enabled** — this is the real requirement. Turn it on at
  Settings → Capabilities → "Code execution and file creation" → ON. The bundled
  validator runs in Claude's sandbox, so **you do not need Python on your own
  computer.**
- **Plan:** works on **Free** plans too, as long as code execution is enabled
  (confirmed in a Claude Desktop session). Pro / Max / Team / Enterprise work as
  well. If you don't see the Skills upload option, make sure code execution is
  turned on first.

## Install (each player does this once)

1. Download **`idd-character-builder.zip`** from this folder.
2. In Claude Desktop (or claude.ai): **Settings → Customize → Skills**.
3. Click **+**, choose **Upload a skill**, and select the ZIP.
4. **Toggle the skill ON.**

## Use it

Just chat:

> "Help me build an In Darkened Dreams character — a sword-and-buckler spellblade."

Claude will run the bundled validator, enforce all the rules, and hand you a
finished, rules-legal character sheet. It also validates existing characters and
levels them up when you earn CP.

## Companion builder app (`character-builder-app.html`)

A standalone, offline interactive builder — a visual companion to the skill.
**Live at https://v3rv.com/idd/dist/character-builder-app.html** (GitHub Pages),
or open the file / a Claude artifact directly. Pick heritage/faction from
dropdowns, step attributes up and down, toggle headers and skills, and watch the
**CP ledger, Vitality, tier, traits, and legality update live**. It embeds the
full ruleset and a JavaScript port of the validator (verified to match the Python
validator's CP math), so it needs no server and no Python.

- **Copy build JSON** → paste into a chat with the skill for the authoritative check / to save.
- **Import JSON** → paste a build from the chat/skill to load it.
- **Save sheet as PDF** → prints just the character sheet, one clean page.

**Handoff with the skill.** The page is sandboxed, so there's no live sync — it's
copy/paste or a pre-filled link:
- *Skill → app:* open a deep link `…character-builder-app.html#build=<url-encoded
  JSON>` (the app loads pre-filled), or paste into **Import JSON**.
- *App → skill:* click **Copy build JSON** and paste it into the chat.

The app's embedded ruleset is regenerated from the master by `dist/build-app.py`
(run automatically by `./build.sh`), so it never drifts from the validator.

## Rebuilding the ZIP (maintainers)

The master copy of the validator + data lives in `plugins/idd/ids-data/`. Never
edit the copies under `dist/idd-character-builder/scripts/` by hand — they're
overwritten on every build. Just run the build script from the repo root:

```bash
./build.sh                 # sync master -> bundle, build the zip, smoke-test it
./build.sh --version 1.0.3 # also stamp 1.0.3 into plugin.json, marketplace.json, SKILL.md
```

`build.sh` syncs the master data into the bundle, (optionally) bumps the version
everywhere, builds `dist/idd-character-builder.zip`, and smoke-tests the packaged
validator (catalog loads, a legal build passes, an illegal build is rejected). The
`.zip` is a build artifact — it's git-ignored and distributed via GitHub Releases:

```bash
gh release create v1.0.3 --title "..." --notes "..." dist/idd-character-builder.zip
```

---

*Unofficial fan-made aid; game content © its creators. See
[`../NOTICE.md`](../NOTICE.md).*

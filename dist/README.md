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

## Rebuilding the ZIP (maintainers)

If you update the data or `SKILL.md`, rebuild the package from the repo root:

```bash
cp plugins/idd/ids-data/*.json plugins/idd/ids-data/validate.py plugins/idd/ids-data/NOTICE.md dist/idd-character-builder/scripts/
cd dist && zip -rq idd-character-builder.zip idd-character-builder -x '*.DS_Store'
```

The `scripts/` folder must contain `validate.py` **and** the JSON data files
together — the validator loads its data from its own directory. Keep the top-level
folder name (`idd-character-builder/`) matching the `name:` in `SKILL.md`.

---

*Unofficial fan-made aid; game content © its creators. See
[`../NOTICE.md`](../NOTICE.md).*

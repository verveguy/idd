---
name: level-up
description: Spend newly-earned Character Points (CP) on an existing In Darkened Dreams (IDD) LARP character — add attributes, headers, skills, or spells while re-validating the whole build stays legal. Use when a player has earned more CP between events and wants to advance/upgrade an existing IDD character.
---

# In Darkened Dreams — Level Up

A player earns more CP as they play IDD. This skill helps them spend newly-earned CP on an **existing** character while keeping the whole build legal (hard-block rules).

## Flow

1. **Load the character.** Use their saved `*.character.json` if they have one. Otherwise reconstruct the current build into the build JSON format (see `build-character` or `validate-character`) and confirm it with the player before changing anything.
2. **Confirm the new CP.** Ask how much CP they earned and add it to `cp_sources` (e.g. bump `earned`, or add a new source). Show the new total available and current remaining.
3. **Baseline check.** Run `python3 .claude/ids-data/validate.py --sheet <build.json>` on the *current* build first, so you both agree on the starting point and remaining CP.
4. **Propose upgrades** within the remaining budget, tailored to how they play the character:
   - Raise an attribute (remember: each level up costs its new value; Earth/Void also raise Vitality).
   - Buy a new header (unlocks its skill tree; check faction gating).
   - Buy skills under owned headers, open skills, or heritage skills.
   - For casters: buy a new Sphere, then spells within it.
   Explain prerequisites and tradeoffs as they come up.
5. **Apply & re-validate.** Add the chosen purchases to the build JSON and re-run `validate.py --sheet`. Because the group hard-blocks illegal builds, do not finalize anything that fails — fix errors with the player and re-run.
6. **Save.** Write the updated `*.character.json` and present the new sheet, noting what changed and the new experience tier if it crossed 50 CP (Experienced) or 100 CP (Accomplished) spent on skills/abilities.

## Notes

- Never remove previously-purchased skills unless the player explicitly wants a rebuild — leveling up is additive.
- Keep a clear before/after: old total vs new total, CP spent this session, CP remaining.
- The validator (`.claude/ids-data/validate.py`) is the source of truth for all CP math and legality. Don't hand-compute.

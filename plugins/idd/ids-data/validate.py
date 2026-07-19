#!/usr/bin/env python3
"""
In Darkened Dreams — character build validator.

Deterministic rules engine for the IDD (Accelerant) character builder skills.
Loads the extracted ruleset JSON in this directory and validates a character
build supplied as JSON (file path or stdin). Enforces the rules as HARD blocks
(the play group's chosen mode): an illegal build fails.

Usage:
    python3 validate.py <build.json>          # validate a build file
    python3 validate.py --catalog             # print the loaded ruleset summary
    python3 validate.py --sheet <build.json>  # validate + print a character sheet
    cat build.json | python3 validate.py -    # read build from stdin

Build JSON format:
{
  "name": "Cornelius",
  "heritage": "Grelkyn",
  "faction": "The Veilward Enclave",
  "cp_sources": {"starting": 30, "history": 5, "bullets": 0, "donations": 0,
                 "playtest": 0, "earned": 0},
  "attributes": {"Air": 2, "Earth": 2, "Fire": 2, "Water": 3, "Void": 3},
  "headers": ["Investigator"],
  "skills": [
    {"name": "Celerity"},
    {"name": "Craftsman", "count": 2}
  ]
}
Skill names are matched case-insensitively against open skills, the chosen
heritage's skills, and the skills of any owned header (including spheres/spells).
"""

import json
import math
import os
import re
import sys

DATA_DIR = os.path.dirname(os.path.abspath(__file__))
ATTRS = ["Air", "Earth", "Fire", "Water", "Void"]
BASE_ATTR = 2


# ---------------------------------------------------------------- data loading
def _load(name):
    with open(os.path.join(DATA_DIR, name), encoding="utf-8") as fh:
        return json.load(fh)


def load_ruleset():
    rs = {
        "rules": _load("rules.json"),
        "heritages": _load("heritages.json"),
        "factions": _load("factions.json"),
        "open_skills": _load("open-skills.json"),
        "magic": _load("magic.json"),
        "spells": {},
        "headers": [],
    }
    if os.path.exists(os.path.join(DATA_DIR, "spells.json")):
        rs["spells"] = _load("spells.json")
    for part in ("headers-a.json", "headers-b.json", "headers-c.json"):
        path = os.path.join(DATA_DIR, part)
        if os.path.exists(path):
            rs["headers"].extend(_load(part))
    return rs


# spheres a character owns are the "The Sphere of <X>" skills they purchased
def sphere_from_skill(name):
    m = re.search(r"sphere of ([\w']+)", _norm(name))
    return m.group(1).capitalize() if m else None


def _all_known_names(rs):
    """Normalized set of every skill/header/heritage-skill/spell name in the game."""
    names = set()
    for s in rs["open_skills"]:
        names.add(_norm(s["name"]))
    for h in rs["heritages"]:
        for s in h.get("heritage_skills", []):
            names.add(_norm(s["name"]))
    for h in rs["headers"]:
        names.add(_norm(h["name"]))
        for s in h["skills"]:
            names.add(_norm(s["name"]))
    for _sphere, spells in rs.get("spells", {}).items():
        for s in spells:
            names.add(_norm(s["name"]))
    return names


def _skill_index(rs):
    """norm-name -> {"name": display name, "desc": description} for every skill/spell."""
    idx = {}

    def put(name, desc):
        idx.setdefault(_norm(name), {"name": name, "desc": desc or ""})

    for s in rs["open_skills"]:
        put(s["name"], s.get("description"))
    for h in rs["heritages"]:
        for s in h.get("heritage_skills", []):
            put(s["name"], s.get("description"))
    for h in rs["headers"]:
        for s in h["skills"]:
            put(s["name"], s.get("description"))
    for _sphere, spells in rs.get("spells", {}).items():
        for s in spells:
            put(s["name"], s.get("description"))
    return idx


def _norm(s):
    """Normalize a name for tolerant matching (case, quotes, punctuation)."""
    s = (s or "").strip().lower()
    s = re.sub(r"[’‘'“”\"]", "", s)          # drop all quote characters
    s = re.sub(r"^[\s.…]+|[\s.…]+$", "", s)  # trim leading/trailing dots/space
    s = re.sub(r"\s+", " ", s)
    return s


# --------------------------------------------------------------- skill catalog
def build_catalog(rs, owned_headers, heritage_name, owned_spheres=None):
    """
    Map normalized skill-name -> record describing where it is buyable from and
    its cost. Includes open skills, the chosen heritage's skills, the skills of
    owned headers, and spells from any owned spheres. Returns (catalog, index).
    """
    catalog = {}

    def add(name, cost, source, repeatable=False, limit=None, prereqs=None,
            spell_like=False):
        catalog[_norm(name)] = {
            "name": name, "cost": cost, "source": source,
            "repeatable": repeatable, "limit": limit,
            "prereqs": prereqs or [], "spell_like": spell_like,
        }

    for s in rs["open_skills"]:
        add(s["name"], s.get("cost", 0), "Open",
            s.get("repeatable", False), s.get("purchase_limit"),
            s.get("prerequisites"))

    # heritage skills for the chosen heritage only
    hn = _norm(heritage_name)
    for h in rs["heritages"]:
        if _norm(h["name"]) == hn or hn in _norm(h["name"]):
            for s in h.get("heritage_skills", []):
                add(s["name"], s.get("cost", 0), f"Heritage:{h['name']}")

    header_index = {_norm(h["name"]): h for h in rs["headers"]}
    for hdr_name in owned_headers:
        h = header_index.get(_norm(hdr_name))
        if not h:
            continue
        for s in h["skills"]:
            add(s["name"], s.get("cost", 0), f"Header:{h['name']}",
                s.get("repeatable", False), s.get("purchase_limit"),
                s.get("prerequisites"), s.get("spell_like", False))

    # spells from owned spheres become buyable (owning the sphere IS the gate)
    for sphere in (owned_spheres or []):
        for sp_name, spells in rs.get("spells", {}).items():
            if _norm(sp_name) == _norm(sphere):
                for sp in spells:
                    add(sp["name"], sp.get("cost", 0), f"Spell:{sphere}",
                        sp.get("repeatable", False), None,
                        [f"The Sphere of {sphere}"], sp.get("spell_like", False))
    return catalog, header_index


# ------------------------------------------------------------------ validation
def attr_cost(value):
    """CP to raise an attribute from the base (2) to `value`: sum of 3..value."""
    if value < BASE_ATTR:
        return 0
    return sum(range(BASE_ATTR + 1, value + 1))


def _faction_prereq(text):
    m = re.search(r"member of (the [\w' ]+?)(?:\.|$| to purchase)", text, re.I)
    return m.group(1).strip() if m else None


def _exclusion_prereq(text):
    m = re.search(r"can\s*not have the ([\w' ]+?) header", text, re.I) or \
        re.search(r"cannot have the ([\w' ]+?) header", text, re.I)
    return m.group(1).strip() if m else None


def validate(build, rs):
    errors, warnings, notes = [], [], []

    # ---- CP available
    sources = build.get("cp_sources", {})
    if not sources and "available_cp" in build:
        available = build["available_cp"]
        sources = {"total": available}
    else:
        available = sum(v for v in sources.values() if isinstance(v, (int, float)))

    # ---- heritage / faction (both are REQUIRED: every IDD character has a
    #      heritage, and must belong to exactly one of the four factions)
    heritage = build.get("heritage")
    heritage_names = [h["name"] for h in rs["heritages"]]
    if not heritage:
        errors.append(
            f"Every character must have a heritage. Choose one of: {heritage_names}.")
    elif not any(_norm(heritage) == _norm(h["name"]) or
                 _norm(heritage) in _norm(h["name"]) for h in rs["heritages"]):
        errors.append(f"Unknown heritage: {heritage!r}. Valid: {heritage_names}")

    faction = build.get("faction")
    faction_names = [f["name"] for f in rs["factions"]]
    if not faction:
        errors.append(
            f"Every character must belong to a faction (the game requires it). "
            f"Choose one of: {faction_names}.")
    elif not any(_norm(faction) == _norm(fn) for fn in faction_names):
        errors.append(f"Unknown faction: {faction!r}. Valid: {faction_names}")

    # ---- attributes
    attrs = {a: build.get("attributes", {}).get(a, BASE_ATTR) for a in ATTRS}
    attr_cp = 0
    for a in ATTRS:
        v = attrs[a]
        if not isinstance(v, int) or v < BASE_ATTR:
            errors.append(f"Attribute {a}={v!r} invalid; minimum is {BASE_ATTR}.")
            v = BASE_ATTR
        attr_cp += attr_cost(v)
    vitality = math.ceil((attrs["Earth"] + attrs["Void"]) / 2)

    # ---- headers
    owned_headers = build.get("headers", [])
    header_index = {_norm(h["name"]): h for h in rs["headers"]}
    header_cp = 0
    resolved_headers = []
    for hdr in owned_headers:
        h = header_index.get(_norm(hdr))
        if not h:
            errors.append(f"Unknown header: {hdr!r}")
            continue
        resolved_headers.append(h["name"])
        header_cp += h.get("header_cost", 0)
        for pre in h.get("prerequisites", []):
            fac = _faction_prereq(pre)
            if fac and (not faction or _norm(fac) != _norm(faction)):
                errors.append(
                    f"Header {h['name']!r} requires faction membership: "
                    f"{fac!r} (you have {faction!r}).")
            exc = _exclusion_prereq(pre)
            if exc and any(_norm(exc) in _norm(o) or _norm(o) in _norm(exc)
                           for o in owned_headers if _norm(o) != _norm(h["name"])):
                errors.append(
                    f"Header {h['name']!r} cannot be taken with the {exc!r} header.")

    # ---- skills (detect owned spheres first so their spells become buyable)
    owned_spheres = []
    for entry in build.get("skills", []):
        nm = entry if isinstance(entry, str) else entry.get("name", "")
        sph = sphere_from_skill(nm)
        if sph:
            owned_spheres.append(sph)
    catalog, _ = build_catalog(rs, resolved_headers, heritage, owned_spheres)
    skills_cp = 0
    purchased = {}   # norm-name -> count
    resolved_skills = []
    for entry in build.get("skills", []):
        if isinstance(entry, str):
            entry = {"name": entry}
        name = entry.get("name", "")
        count = entry.get("count", 1)
        rec = catalog.get(_norm(name))
        if not rec:
            errors.append(
                f"Skill {name!r} is not available. Either it doesn't exist, or "
                f"you haven't purchased the header/heritage that grants it.")
            continue
        if count > 1 and not rec["repeatable"]:
            errors.append(f"Skill {rec['name']!r} is not repeatable (count={count}).")
            count = 1
        if rec["limit"] is not None and count > rec["limit"]:
            errors.append(
                f"Skill {rec['name']!r} exceeds its purchase limit "
                f"({count} > {rec['limit']}).")
        if _norm(name) in purchased and not rec["repeatable"]:
            errors.append(f"Duplicate purchase of non-repeatable skill {rec['name']!r}.")
        purchased[_norm(name)] = purchased.get(_norm(name), 0) + count
        skills_cp += rec["cost"] * count
        resolved_skills.append((rec, count))

    # ---- skill prerequisites
    # A prereq LIST is treated as OR (satisfied if any element is owned) — this
    # matches how the data stores alternatives, e.g. ["Evasive","Mystic Armor"].
    #  * single element that names a known skill/header, not owned -> hard ERROR
    #  * OR-list (>1 element), none owned                          -> warning
    #  * prose element ("2 purchases of...", "any Melee Weapon...") -> warning
    owned_trait_targets = set(purchased) | {_norm(h) for h in resolved_headers}
    all_known = _all_known_names(rs)

    def _owned(pre, self_name):
        pn = _norm(pre)
        if pn == self_name:          # a skill never satisfies its own prereq
            return False
        if pn in owned_trait_targets:
            return True
        # an owned name that is a prefix of the prereq phrase counts, e.g.
        # owning "Melee Weapon" satisfies the prereq "Melee Weapon - Staff"
        return any(pn.startswith(t + " ") or pn.startswith(t + "-")
                   for t in owned_trait_targets)

    for rec, _count in resolved_skills:
        prereqs = rec["prereqs"]
        self_n = _norm(rec["name"])
        if not prereqs or any(_owned(p, self_n) for p in prereqs):
            continue
        if len(prereqs) == 1 and _norm(prereqs[0]) in all_known:
            errors.append(
                f"Skill {rec['name']!r} requires prerequisite {prereqs[0]!r}, "
                f"which is not in your build.")
        elif len(prereqs) > 1 and all(_norm(p) in all_known for p in prereqs):
            errors.append(
                f"Skill {rec['name']!r} requires one of {prereqs} — "
                f"you have none of them.")
        else:
            warnings.append(
                f"Skill {rec['name']!r} lists prerequisite {prereqs[0]!r} that "
                f"couldn't be auto-verified — check the rulebook.")

    # ---- mutual exclusions ("You cannot purchase this skill if you have X or Y")
    # Parsed from descriptions so new exclusions are caught automatically.
    index = _skill_index(rs)   # norm-name -> {"name": display, "desc": text}
    reported = set()
    for rec, _count in resolved_skills:
        self_norm = _norm(rec["name"])
        text = index.get(self_norm, {}).get("desc", "")
        m = re.search(r"cannot purchase this (?:skill|spell)"
                      r"(?:,[^,]+,)? if you (?:have|already have|possess) (.+?)\.",
                      text, re.I)
        if not m:
            continue
        for cand in re.split(r",|\bor\b|\band\b", m.group(1)):
            cn = _norm(cand)
            if cn and cn != self_norm and cn in purchased:
                pair = frozenset((self_norm, cn))
                if pair in reported:
                    continue
                reported.add(pair)
                other = index.get(cn, {}).get("name", cand.strip())
                errors.append(
                    f"Skills {rec['name']!r} and {other!r} are mutually exclusive "
                    f"— a character may have only one.")

    total_spent = attr_cp + header_cp + skills_cp
    if total_spent > available:
        errors.append(
            f"Over budget: spent {total_spent} CP but only {available} CP available "
            f"(attributes {attr_cp} + headers {header_cp} + skills {skills_cp}).")

    # experience tier is based on CP spent on skills & abilities (headers + skills)
    ability_cp = header_cp + skills_cp
    tier = "Initiate"
    if ability_cp >= 100:
        tier = "Accomplished"
    elif ability_cp >= 50:
        tier = "Experienced"

    return {
        "valid": not errors,
        "errors": errors,
        "warnings": warnings,
        "notes": notes,
        "available_cp": available,
        "cp_sources": sources,
        "attr_cp": attr_cp,
        "header_cp": header_cp,
        "skills_cp": skills_cp,
        "total_spent": total_spent,
        "cp_remaining": available - total_spent,
        "attributes": attrs,
        "vitality": vitality,
        "ability_cp": ability_cp,
        "tier": tier,
        "resolved_headers": resolved_headers,
        "resolved_skills": [(r["name"], c, r["cost"]) for r, c in resolved_skills],
        "traits": _derive_traits(build, resolved_headers, rs, ability_cp, tier),
    }


def _derive_traits(build, resolved_headers, rs, ability_cp, tier):
    traits = ["Living"]
    if build.get("heritage"):
        traits.append(build["heritage"])
    if build.get("faction"):
        traits.append(build["faction"])
    header_index = {_norm(h["name"]): h for h in rs["headers"]}
    for hdr in resolved_headers:
        traits.append(hdr)
        h = header_index.get(_norm(hdr))
        for t in (h.get("grants_traits", []) if h else []):
            if t not in traits:
                traits.append(t)
    traits.append(tier)
    return traits


# ------------------------------------------------------------------- reporting
def render_sheet(build, res):
    L = []
    L.append("=" * 60)
    L.append(f"  {build.get('name', 'Unnamed Character')}")
    L.append("=" * 60)
    L.append(f"  Heritage : {build.get('heritage', '-')}")
    L.append(f"  Faction  : {build.get('faction', '-')}")
    L.append(f"  Vitality : {res['vitality']}    Tier: {res['tier']}")
    L.append("")
    L.append("  Attributes:")
    for a in ATTRS:
        L.append(f"    {a:<6} {res['attributes'][a]}")
    L.append("")
    L.append("  CP Budget:")
    for k, v in res["cp_sources"].items():
        L.append(f"    {k:<12} {v}")
    L.append(f"    {'-'*20}")
    L.append(f"    available    {res['available_cp']}")
    L.append(f"    attributes  -{res['attr_cp']}")
    L.append(f"    headers     -{res['header_cp']}")
    L.append(f"    skills      -{res['skills_cp']}")
    L.append(f"    {'-'*20}")
    L.append(f"    REMAINING    {res['cp_remaining']}")
    L.append("")
    L.append("  Headers:")
    for h in res["resolved_headers"]:
        L.append(f"    + {h}")
    L.append("")
    L.append("  Skills:")
    for name, count, cost in res["resolved_skills"]:
        tag = f" x{count}" if count > 1 else ""
        L.append(f"    - {name}{tag}  ({cost * count} CP)")
    L.append("")
    L.append(f"  Traits: {', '.join(res['traits'])}")
    L.append("=" * 60)
    return "\n".join(L)


def print_report(res):
    if res["valid"]:
        print(f"VALID BUILD — {res['total_spent']}/{res['available_cp']} CP spent, "
              f"{res['cp_remaining']} remaining.")
    else:
        print(f"INVALID BUILD — {len(res['errors'])} error(s).")
    for e in res["errors"]:
        print(f"  ERROR: {e}")
    for w in res["warnings"]:
        print(f"  warn:  {w}")
    for n in res["notes"]:
        print(f"  note:  {n}")


def print_catalog(rs):
    print(f"Ruleset: {rs['rules']['game']} v{rs['rules'].get('ruleset_version')}")
    print(f"Heritages ({len(rs['heritages'])}): "
          + ", ".join(h["name"] for h in rs["heritages"]))
    print(f"Factions ({len(rs['factions'])}): "
          + ", ".join(f["name"] for f in rs["factions"]))
    print(f"Open skills ({len(rs['open_skills'])})")
    print(f"Headers ({len(rs['headers'])}):")
    for h in rs["headers"]:
        pr = f"  [prereq: {h['prerequisites']}]" if h.get("prerequisites") else ""
        print(f"  - {h['name']} ({h['header_cost']} CP, {len(h['skills'])} skills){pr}")


def main(argv):
    if "--catalog" in argv:
        print_catalog(load_ruleset())
        return 0
    sheet = "--sheet" in argv
    argv = [a for a in argv if a != "--sheet"]
    if len(argv) < 2:
        print(__doc__)
        return 1
    src = argv[1]
    raw = sys.stdin.read() if src == "-" else open(src, encoding="utf-8").read()
    build = json.loads(raw)
    rs = load_ruleset()
    res = validate(build, rs)
    if sheet and res["valid"]:
        print(render_sheet(build, res))
        print()
    print_report(res)
    return 0 if res["valid"] else 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))

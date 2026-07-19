#!/usr/bin/env python3
"""
build-app.py — refresh the companion builder app's embedded ruleset data.

The interactive builder (dist/character-builder-app.html) embeds a compact copy
of the ruleset so it runs offline. This script regenerates that payload from the
MASTER data in plugins/idd/ids-data/ and injects it into the app's
<script id="idd-data"> block, in place. Run it (or ./build.sh) after any data
change so the app never drifts from the validator.
"""
import json, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "plugins", "idd", "ids-data"))
import validate as V

APP = os.path.join(ROOT, "dist", "character-builder-app.html")


def eff(desc, n=120):
    d = re.sub(r"\s+", " ", (desc or "").strip())
    m = re.split(r'(?<=[.\"”])\s', d)
    out = m[0] if m else d
    return (out[:n - 1].rstrip() + "…") if len(out) > n else out


def attr_str(a):
    return ", ".join(f"{x['amount']} {x['attribute']}" for x in a) if a else ""


def conflicts(desc):
    m = re.search(r"cannot purchase this (?:skill|spell)(?:,[^,]+,)? if you "
                  r"(?:have|already have|possess) (.+?)\.", desc or "", re.I)
    return [c.strip() for c in re.split(r",|\bor\b|\band\b", m.group(1)) if c.strip()] if m else []


def sphere_of(name):
    m = re.search(r"sphere of (\w+)", name.lower())
    return m.group(1).capitalize() if m else None


def faction_of(h):
    for p in h.get("prerequisites", []):
        m = re.search(r"member of (the [\w' ]+?)(?:\.|$| to)", p, re.I)
        if m:
            return m.group(1).strip()
    return None


def excludes(h):
    out = []
    for p in h.get("prerequisites", []):
        m = re.search(r"can\s*not have the ([\w' ]+?) header", p, re.I) or \
            re.search(r"cannot have the ([\w' ]+?) header", p, re.I)
        if m:
            out.append(m.group(1).strip())
    return out


def sk(s):
    d = {"n": s["name"], "c": s.get("cost", 0)}
    if s.get("repeatable"):
        d["rep"] = 1
    if s.get("purchase_limit"):
        d["lim"] = s["purchase_limit"]
    if s.get("prerequisites"):
        d["pre"] = s["prerequisites"]
    a = attr_str(s.get("attribute_costs", []))
    if a:
        d["a"] = a
    if s.get("spell_like"):
        d["sp"] = 1
    c = conflicts(s.get("description", ""))
    if c:
        d["x"] = c
    sph = sphere_of(s["name"])
    if sph:
        d["sphere"] = sph
    d["e"] = eff(s.get("description", ""))
    return d


def payload():
    rs = V.load_ruleset()
    return {
        "rules": {"start": 30, "history": 5, "attrs": ["Air", "Earth", "Fire", "Water", "Void"], "base": 2},
        "heritages": [{"n": h["name"],
                       "skills": [{"n": x["name"], "c": x.get("cost", 0), "e": eff(x.get("description", ""))}
                                  for x in h.get("heritage_skills", [])],
                       "disadv": eff("; ".join(h.get("disadvantages", [])), 140) if h.get("disadvantages") else ""}
                      for h in rs["heritages"]],
        "factions": [f["name"] for f in rs["factions"]],
        "headers": [{"n": h["name"], "c": h["header_cost"], "fac": faction_of(h),
                     "grants": h.get("grants_traits", []), "excl": excludes(h),
                     "skills": [sk(s) for s in h["skills"]]} for h in rs["headers"]],
        "open": [sk(s) for s in rs["open_skills"]],
        "spells": {sph: [sk(s) for s in sl] for sph, sl in rs.get("spells", {}).items()},
    }


def main():
    data = json.dumps(payload(), ensure_ascii=False, separators=(",", ":"))
    html = open(APP, encoding="utf-8").read()
    pat = re.compile(r'(<script id="idd-data" type="application/json">).*?(</script>)', re.S)
    if not pat.search(html):
        print("error: idd-data script block not found in app", file=sys.stderr)
        return 1
    # function replacement → returned string is used literally (no backslash processing)
    html = pat.sub(lambda m: m.group(1) + data + m.group(2), html, count=1)
    open(APP, "w", encoding="utf-8").write(html)
    # verify it still parses
    m = re.search(r'<script id="idd-data" type="application/json">(.*?)</script>', html, re.S)
    json.loads(m.group(1))
    print(f"refreshed app data: {len(data)} bytes embedded, JSON valid")
    return 0


if __name__ == "__main__":
    sys.exit(main())

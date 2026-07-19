package main

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/verveguy/idd/mcp-app/rules"
)

// uiDataJSON builds the compact ruleset payload embedded in the builder UI,
// mirroring dist/build-app.py so the applet renders the same catalog offline.

type uiSkill struct {
	N      string   `json:"n"`
	C      int      `json:"c"`
	Rep    bool     `json:"rep,omitempty"`
	Lim    int      `json:"lim,omitempty"`
	Pre    []string `json:"pre,omitempty"`
	A      string   `json:"a,omitempty"`
	Sp     bool     `json:"sp,omitempty"`
	X      []string `json:"x,omitempty"`
	Sphere string   `json:"sphere,omitempty"`
	E      string   `json:"e"`
}

type uiHeritage struct {
	N      string    `json:"n"`
	Skills []uiSkill `json:"skills"`
	Disadv string    `json:"disadv,omitempty"`
}

type uiHeader struct {
	N      string    `json:"n"`
	C      int       `json:"c"`
	Fac    string    `json:"fac,omitempty"`
	Grants []string  `json:"grants,omitempty"`
	Excl   []string  `json:"excl,omitempty"`
	Skills []uiSkill `json:"skills"`
}

type uiPayload struct {
	Rules     map[string]any        `json:"rules"`
	Heritages []uiHeritage          `json:"heritages"`
	Factions  []string              `json:"factions"`
	Headers   []uiHeader            `json:"headers"`
	Open      []uiSkill             `json:"open"`
	Spells    map[string][]uiSkill  `json:"spells"`
}

var (
	reSentence = regexp.MustCompile(`[.!?”"]\s`)
	reWSp      = regexp.MustCompile(`\s+`)
	reMemberU  = regexp.MustCompile(`(?i)member of (the [\w' ]+?)(?:\.|$| to)`)
	reExclU    = regexp.MustCompile(`(?i)can\s*not have the ([\w' ]+?) header`)
	reSphereU  = regexp.MustCompile(`(?i)sphere of (\w+)`)
	reConfU    = regexp.MustCompile(`(?i)cannot purchase this (?:skill|spell)(?:,[^,]+,)? if you (?:have|already have|possess) (.+?)\.`)
	reConfBrU  = regexp.MustCompile(`,|\bor\b|\band\b`)
)

func eff(desc string) string {
	d := strings.TrimSpace(reWSp.ReplaceAllString(desc, " "))
	if loc := reSentence.FindStringIndex(d); loc != nil {
		// match is <punct><space>; cut before the space (loc[1]-1) so a
		// multibyte punctuation char (e.g. ”) is kept whole — never split mid-rune.
		d = strings.TrimSpace(d[:loc[1]-1])
	}
	if r := []rune(d); len(r) > 120 {
		d = strings.TrimSpace(string(r[:119])) + "…"
	}
	return d
}

func attrStr(a []rules.AttrCost) string {
	if len(a) == 0 {
		return ""
	}
	parts := make([]string, len(a))
	for i, x := range a {
		parts[i] = itoa(x.Amount) + " " + x.Attribute
	}
	return strings.Join(parts, ", ")
}

func itoa(n int) string { return strings.TrimSpace(jsonNum(n)) }
func jsonNum(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func conflicts(desc string) []string {
	m := reConfU.FindStringSubmatch(desc)
	if m == nil {
		return nil
	}
	var out []string
	for _, c := range reConfBrU.Split(m[1], -1) {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func sphereOfU(name string) string {
	if m := reSphereU.FindStringSubmatch(name); m != nil {
		s := strings.ToLower(m[1])
		return strings.ToUpper(s[:1]) + s[1:]
	}
	return ""
}

func toUISkill(s rules.Skill) uiSkill {
	u := uiSkill{N: s.Name, C: s.Cost, Rep: s.Repeatable, Pre: s.Prerequisites,
		A: attrStr(s.AttributeCost), Sp: s.SpellLike, X: conflicts(s.Description),
		Sphere: sphereOfU(s.Name), E: eff(s.Description)}
	if s.PurchaseLimit != nil {
		u.Lim = *s.PurchaseLimit
	}
	return u
}

func uiDataJSON() string {
	p := uiPayload{
		Rules:    map[string]any{"start": 30, "history": 5, "attrs": rules.Attrs, "base": 2},
		Factions: []string{},
		Spells:   map[string][]uiSkill{},
	}
	for _, h := range ruleset.Heritages {
		uh := uiHeritage{N: h.Name}
		for _, s := range h.HeritageSkills {
			uh.Skills = append(uh.Skills, uiSkill{N: s.Name, C: s.Cost, E: eff(s.Description)})
		}
		if len(h.Disadvantages) > 0 {
			uh.Disadv = eff(strings.Join(h.Disadvantages, "; "))
		}
		p.Heritages = append(p.Heritages, uh)
	}
	for _, f := range ruleset.Factions {
		p.Factions = append(p.Factions, f.Name)
	}
	for i := range ruleset.Headers {
		h := &ruleset.Headers[i]
		uh := uiHeader{N: h.Name, C: h.HeaderCost, Grants: h.GrantsTraits}
		if m := reMemberU.FindStringSubmatch(strings.Join(h.Prerequisites, " ")); m != nil {
			uh.Fac = strings.TrimSpace(m[1])
		}
		for _, p := range h.Prerequisites {
			if m := reExclU.FindStringSubmatch(p); m != nil {
				uh.Excl = append(uh.Excl, strings.TrimSpace(m[1]))
			}
		}
		for _, s := range h.Skills {
			uh.Skills = append(uh.Skills, toUISkill(s))
		}
		p.Headers = append(p.Headers, uh)
	}
	for _, s := range ruleset.OpenSkills {
		p.Open = append(p.Open, toUISkill(s))
	}
	for sphere, spells := range ruleset.Spells {
		for _, s := range spells {
			p.Spells[sphere] = append(p.Spells[sphere], toUISkill(s))
		}
	}
	b, _ := json.Marshal(p)
	return string(b)
}

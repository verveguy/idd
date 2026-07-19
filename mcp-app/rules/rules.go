// Package rules is a Go port of the In Darkened Dreams character validator
// (plugins/idd/ids-data/validate.py). It embeds the verified ruleset data and
// enforces the same hard-block rules, for use by the IDD MCP App server.
package rules

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

//go:embed data/*.json
var dataFS embed.FS

// Attributes are fixed by the game.
var Attrs = []string{"Air", "Earth", "Fire", "Water", "Void"}

const baseAttr = 2

// ---------- data model ----------

type AttrCost struct {
	Attribute string `json:"attribute"`
	Amount    int    `json:"amount"`
}

type Skill struct {
	Name          string     `json:"name"`
	Cost          int        `json:"cost"`
	Repeatable    bool       `json:"repeatable"`
	PurchaseLimit *int       `json:"purchase_limit"`
	Prerequisites []string   `json:"prerequisites"`
	AttributeCost []AttrCost `json:"attribute_costs"`
	SpellLike     bool       `json:"spell_like"`
	Description   string     `json:"description"`
}

type Header struct {
	Name          string   `json:"name"`
	HeaderCost    int      `json:"header_cost"`
	Prerequisites []string `json:"prerequisites"`
	GrantsTraits  []string `json:"grants_traits"`
	Notes         string   `json:"notes"`
	Skills        []Skill  `json:"skills"`
}

type HeritageSkill struct {
	Name        string `json:"name"`
	Cost        int    `json:"cost"`
	Description string `json:"description"`
}

type Heritage struct {
	Name           string          `json:"name"`
	Disadvantages  []string        `json:"disadvantages"`
	HeritageSkills []HeritageSkill `json:"heritage_skills"`
}

type Faction struct {
	Name string `json:"name"`
}

type Ruleset struct {
	Heritages  []Heritage
	Factions   []Faction
	OpenSkills []Skill
	Spells     map[string][]Skill
	Headers    []Header
	byHeader   map[string]*Header
	allNames   map[string]bool
	descByName map[string]string // norm -> description
	dispByName map[string]string // norm -> display name
}

// ---------- loading ----------

func load(name string, v any) error {
	b, err := dataFS.ReadFile("data/" + name)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Load reads the embedded ruleset.
func Load() (*Ruleset, error) {
	rs := &Ruleset{
		byHeader:   map[string]*Header{},
		allNames:   map[string]bool{},
		descByName: map[string]string{},
		dispByName: map[string]string{},
	}
	if err := load("heritages.json", &rs.Heritages); err != nil {
		return nil, err
	}
	if err := load("factions.json", &rs.Factions); err != nil {
		return nil, err
	}
	if err := load("open-skills.json", &rs.OpenSkills); err != nil {
		return nil, err
	}
	if err := load("spells.json", &rs.Spells); err != nil {
		return nil, err
	}
	for _, f := range []string{"headers-a.json", "headers-b.json", "headers-c.json"} {
		var hs []Header
		if err := load(f, &hs); err != nil {
			return nil, err
		}
		rs.Headers = append(rs.Headers, hs...)
	}
	// indexes
	index := func(name, desc string) {
		n := norm(name)
		rs.allNames[n] = true
		if _, ok := rs.descByName[n]; !ok {
			rs.descByName[n] = desc
			rs.dispByName[n] = name
		}
	}
	for _, s := range rs.OpenSkills {
		index(s.Name, s.Description)
	}
	for _, h := range rs.Heritages {
		for _, s := range h.HeritageSkills {
			index(s.Name, s.Description)
		}
	}
	for i := range rs.Headers {
		h := &rs.Headers[i]
		rs.byHeader[norm(h.Name)] = h
		rs.allNames[norm(h.Name)] = true
		for _, s := range h.Skills {
			index(s.Name, s.Description)
		}
	}
	for _, spells := range rs.Spells {
		for _, s := range spells {
			index(s.Name, s.Description)
		}
	}
	return rs, nil
}

// ---------- normalization / helpers ----------

var (
	reQuotes = regexp.MustCompile(`[’‘'“”"]`)
	reTrim   = regexp.MustCompile(`^[\s.…]+|[\s.…]+$`)
	reWS     = regexp.MustCompile(`\s+`)
	reMember = regexp.MustCompile(`(?i)member of (the [\w' ]+?)(?:\.|$| to)`)
	reExcl   = regexp.MustCompile(`(?i)can\s*not have the ([\w' ]+?) header`)
	reSphere = regexp.MustCompile(`(?i)sphere of (\w+)`)
	reConf   = regexp.MustCompile(`(?i)cannot purchase this (?:skill|spell)(?:,[^,]+,)? if you (?:have|already have|possess) (.+?)\.`)
	reConfBr = regexp.MustCompile(`,|\bor\b|\band\b`)
)

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = reQuotes.ReplaceAllString(s, "")
	s = reTrim.ReplaceAllString(s, "")
	s = reWS.ReplaceAllString(s, " ")
	return s
}

func attrCost(v int) int {
	t := 0
	for i := baseAttr + 1; i <= v; i++ {
		t += i
	}
	return t
}

func factionOf(h *Header) string {
	for _, p := range h.Prerequisites {
		if m := reMember.FindStringSubmatch(p); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func excludesOf(h *Header) []string {
	var out []string
	for _, p := range h.Prerequisites {
		if m := reExcl.FindStringSubmatch(p); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

func sphereOf(name string) string {
	if m := reSphere.FindStringSubmatch(name); m != nil {
		s := strings.ToLower(m[1])
		return strings.ToUpper(s[:1]) + s[1:]
	}
	return ""
}

// ---------- build input ----------

// SkillRef accepts either "Name" or {"name":..., "count":...} in JSON.
type SkillRef struct {
	Name  string
	Count int
}

func (s *SkillRef) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		s.Name, s.Count = str, 1
		return nil
	}
	var obj struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	s.Name = obj.Name
	s.Count = obj.Count
	if s.Count == 0 {
		s.Count = 1
	}
	return nil
}

type Build struct {
	Name        string         `json:"name"`
	Heritage    string         `json:"heritage"`
	Faction     string         `json:"faction"`
	CPSources   map[string]int `json:"cp_sources"`
	AvailableCP *int           `json:"available_cp"`
	Attributes  map[string]int `json:"attributes"`
	Headers     []string       `json:"headers"`
	Skills      []SkillRef     `json:"skills"`
}

// ---------- result ----------

type ResolvedSkill struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Cost  int    `json:"cost"`
}

type Result struct {
	Valid          bool            `json:"valid"`
	Errors         []string        `json:"errors"`
	Warnings       []string        `json:"warnings"`
	Available      int             `json:"available_cp"`
	AttrCP         int             `json:"attr_cp"`
	HeaderCP       int             `json:"header_cp"`
	SkillsCP       int             `json:"skills_cp"`
	Total          int             `json:"total_spent"`
	Remaining      int             `json:"cp_remaining"`
	Attributes     map[string]int  `json:"attributes"`
	Vitality       int             `json:"vitality"`
	Tier           string          `json:"tier"`
	Traits         []string        `json:"traits"`
	ResolvedSkills []ResolvedSkill `json:"resolved_skills"`
}

type catRec struct {
	skill  Skill
	source string
}

func (rs *Ruleset) catalog(ownedHeaders []string, heritage string, ownedSpheres []string) map[string]catRec {
	cat := map[string]catRec{}
	add := func(s Skill, src string) { cat[norm(s.Name)] = catRec{s, src} }
	for _, s := range rs.OpenSkills {
		add(s, "Open")
	}
	hn := norm(heritage)
	for _, h := range rs.Heritages {
		if norm(h.Name) == hn || (hn != "" && strings.Contains(norm(h.Name), hn)) {
			for _, s := range h.HeritageSkills {
				add(Skill{Name: s.Name, Cost: s.Cost, Description: s.Description}, "Heritage:"+h.Name)
			}
		}
	}
	for _, hName := range ownedHeaders {
		if h := rs.byHeader[norm(hName)]; h != nil {
			for _, s := range h.Skills {
				add(s, "Header:"+h.Name)
			}
		}
	}
	for _, sp := range ownedSpheres {
		for name, spells := range rs.Spells {
			if norm(name) == norm(sp) {
				for _, s := range spells {
					add(s, "Spell:"+sp)
				}
			}
		}
	}
	return cat
}

// Validate applies the hard-block rules and returns a full report.
func (rs *Ruleset) Validate(b Build) Result {
	var errors, warnings []string
	adderr := func(f string, a ...any) { errors = append(errors, fmt.Sprintf(f, a...)) }
	addwarn := func(f string, a ...any) { warnings = append(warnings, fmt.Sprintf(f, a...)) }

	// available CP
	available := 0
	if len(b.CPSources) == 0 && b.AvailableCP != nil {
		available = *b.AvailableCP
	} else {
		for _, v := range b.CPSources {
			available += v
		}
	}

	// heritage / faction required
	heritageNames := names(func() []string {
		var o []string
		for _, h := range rs.Heritages {
			o = append(o, h.Name)
		}
		return o
	}())
	if b.Heritage == "" {
		adderr("Every character must have a heritage. Choose one of: %s.", heritageNames)
	} else if !anyMatch(rs.Heritages, func(h Heritage) bool { return norm(h.Name) == norm(b.Heritage) || strings.Contains(norm(h.Name), norm(b.Heritage)) }) {
		adderr("Unknown heritage: %q. Valid: %s", b.Heritage, heritageNames)
	}
	factionNames := names(func() []string {
		var o []string
		for _, f := range rs.Factions {
			o = append(o, f.Name)
		}
		return o
	}())
	if b.Faction == "" {
		adderr("Every character must belong to a faction (the game requires it). Choose one of: %s.", factionNames)
	} else if !anyMatch(rs.Factions, func(f Faction) bool { return norm(f.Name) == norm(b.Faction) }) {
		adderr("Unknown faction: %q. Valid: %s", b.Faction, factionNames)
	}

	// attributes
	attrs := map[string]int{}
	attrCP := 0
	for _, a := range Attrs {
		v, ok := b.Attributes[a]
		if !ok {
			v = baseAttr
		}
		if v < baseAttr {
			adderr("Attribute %s=%d invalid; minimum is %d.", a, v, baseAttr)
			v = baseAttr
		}
		attrs[a] = v
		attrCP += attrCost(v)
	}
	vitality := int(math.Ceil(float64(attrs["Earth"]+attrs["Void"]) / 2))

	// headers
	headerCP := 0
	var resolvedHeaders []string
	for _, hName := range b.Headers {
		h := rs.byHeader[norm(hName)]
		if h == nil {
			adderr("Unknown header: %q", hName)
			continue
		}
		resolvedHeaders = append(resolvedHeaders, h.Name)
		headerCP += h.HeaderCost
		if fac := factionOf(h); fac != "" && norm(fac) != norm(b.Faction) {
			adderr("Header %q requires faction membership: %q (you have %q).", h.Name, fac, b.Faction)
		}
		for _, exc := range excludesOf(h) {
			for _, o := range b.Headers {
				if norm(o) == norm(h.Name) {
					continue
				}
				if strings.Contains(norm(exc), norm(o)) || strings.Contains(norm(o), norm(exc)) {
					adderr("Header %q cannot be taken with the %q header.", h.Name, exc)
				}
			}
		}
	}

	// owned spheres from skills
	var ownedSpheres []string
	for _, sr := range b.Skills {
		if sp := sphereOf(sr.Name); sp != "" {
			ownedSpheres = append(ownedSpheres, sp)
		}
	}
	cat := rs.catalog(resolvedHeaders, b.Heritage, ownedSpheres)

	// skills
	skillsCP := 0
	purchased := map[string]int{}
	var resolved []struct {
		rec Skill
		cnt int
	}
	for _, sr := range b.Skills {
		key := norm(sr.Name)
		rec, ok := cat[key]
		if !ok {
			adderr("Skill %q is not available. Either it doesn't exist, or you haven't purchased the header/heritage that grants it.", sr.Name)
			continue
		}
		cnt := sr.Count
		if cnt < 1 {
			cnt = 1
		}
		if cnt > 1 && !rec.skill.Repeatable {
			adderr("Skill %q is not repeatable (count=%d).", rec.skill.Name, cnt)
			cnt = 1
		}
		if rec.skill.PurchaseLimit != nil && cnt > *rec.skill.PurchaseLimit {
			adderr("Skill %q exceeds its purchase limit (%d > %d).", rec.skill.Name, cnt, *rec.skill.PurchaseLimit)
		}
		if _, dup := purchased[key]; dup && !rec.skill.Repeatable {
			adderr("Duplicate purchase of non-repeatable skill %q.", rec.skill.Name)
		}
		purchased[key] += cnt
		skillsCP += rec.skill.Cost * cnt
		resolved = append(resolved, struct {
			rec Skill
			cnt int
		}{rec.skill, cnt})
	}

	// prerequisites
	ownedTargets := map[string]bool{}
	for k := range purchased {
		ownedTargets[k] = true
	}
	for _, h := range resolvedHeaders {
		ownedTargets[norm(h)] = true
	}
	owned := func(pre, self string) bool {
		pn := norm(pre)
		if pn == self {
			return false
		}
		if ownedTargets[pn] {
			return true
		}
		for t := range ownedTargets {
			if strings.HasPrefix(pn, t+" ") || strings.HasPrefix(pn, t+"-") {
				return true
			}
		}
		return false
	}
	for _, r := range resolved {
		pre := r.rec.Prerequisites
		if len(pre) == 0 {
			continue
		}
		self := norm(r.rec.Name)
		satisfied := false
		for _, p := range pre {
			if owned(p, self) {
				satisfied = true
				break
			}
		}
		if satisfied {
			continue
		}
		if len(pre) == 1 && rs.allNames[norm(pre[0])] {
			adderr("Skill %q requires prerequisite %q, which is not in your build.", r.rec.Name, pre[0])
		} else if len(pre) > 1 && allKnown(rs, pre) {
			adderr("Skill %q requires one of %v — you have none of them.", r.rec.Name, pre)
		} else {
			addwarn("Skill %q lists prerequisite %q that couldn't be auto-verified — check the rulebook.", r.rec.Name, pre[0])
		}
	}

	// mutual exclusions (parsed from descriptions)
	reported := map[string]bool{}
	for _, r := range resolved {
		self := norm(r.rec.Name)
		text := rs.descByName[self]
		m := reConf.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		for _, cand := range reConfBr.Split(m[1], -1) {
			cn := norm(cand)
			if cn == "" || cn == self {
				continue
			}
			if _, has := purchased[cn]; has {
				pairKey := pair(self, cn)
				if reported[pairKey] {
					continue
				}
				reported[pairKey] = true
				other := cn
				if d, ok := rs.dispByName[cn]; ok {
					other = d
				}
				adderr("Skills %q and %q are mutually exclusive — a character may have only one.", r.rec.Name, other)
			}
		}
	}

	total := attrCP + headerCP + skillsCP
	if total > available {
		adderr("Over budget: spent %d CP but only %d CP available (attributes %d + headers %d + skills %d).",
			total, available, attrCP, headerCP, skillsCP)
	}

	abilityCP := headerCP + skillsCP
	tier := "Initiate"
	if abilityCP >= 100 {
		tier = "Accomplished"
	} else if abilityCP >= 50 {
		tier = "Experienced"
	}

	// traits
	traits := []string{"Living"}
	if b.Heritage != "" {
		traits = append(traits, b.Heritage)
	}
	if b.Faction != "" {
		traits = append(traits, b.Faction)
	}
	for _, hName := range resolvedHeaders {
		traits = append(traits, hName)
		if h := rs.byHeader[norm(hName)]; h != nil {
			for _, t := range h.GrantsTraits {
				if !contains(traits, t) {
					traits = append(traits, t)
				}
			}
		}
	}
	traits = append(traits, tier)

	var rsk []ResolvedSkill
	for _, r := range resolved {
		rsk = append(rsk, ResolvedSkill{r.rec.Name, r.cnt, r.rec.Cost * r.cnt})
	}

	return Result{
		Valid: len(errors) == 0, Errors: errors, Warnings: warnings,
		Available: available, AttrCP: attrCP, HeaderCP: headerCP, SkillsCP: skillsCP,
		Total: total, Remaining: available - total, Attributes: attrs,
		Vitality: vitality, Tier: tier, Traits: traits, ResolvedSkills: rsk,
	}
}

// ---------- small helpers ----------

func pair(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}
func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
func allKnown(rs *Ruleset, pre []string) bool {
	for _, p := range pre {
		if !rs.allNames[norm(p)] {
			return false
		}
	}
	return true
}
func names(ss []string) string {
	sort.SliceStable(ss, func(i, j int) bool { return false })
	return "[" + strings.Join(ss, ", ") + "]"
}
func anyMatch[T any](xs []T, f func(T) bool) bool {
	for _, x := range xs {
		if f(x) {
			return true
		}
	}
	return false
}

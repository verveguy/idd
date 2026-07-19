// Command validate is a CLI wrapper around the Go rules engine, mirroring
// plugins/idd/ids-data/validate.py so the two can be parity-tested.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/verveguy/idd/mcp-app/rules"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: validate <build.json>")
		os.Exit(1)
	}
	rs, err := rules.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "load error:", err)
		os.Exit(1)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read error:", err)
		os.Exit(1)
	}
	var b rules.Build
	if err := json.Unmarshal(raw, &b); err != nil {
		fmt.Fprintln(os.Stderr, "json error:", err)
		os.Exit(1)
	}
	r := rs.Validate(b)
	if r.Valid {
		fmt.Printf("VALID BUILD — %d/%d CP spent, %d remaining.\n", r.Total, r.Available, r.Remaining)
	} else {
		fmt.Printf("INVALID BUILD — %d error(s).\n", len(r.Errors))
		for _, e := range r.Errors {
			fmt.Println("  ERROR:", e)
		}
	}
	for _, w := range r.Warnings {
		fmt.Println("  warn: ", w)
	}
	// compact machine line for parity diffing
	fmt.Printf("::stats total=%d attr=%d hdr=%d sk=%d rem=%d V=%d tier=%s errs=%d warn=%d\n",
		r.Total, r.AttrCP, r.HeaderCP, r.SkillsCP, r.Remaining, r.Vitality, r.Tier, len(r.Errors), len(r.Warnings))
	if !r.Valid {
		os.Exit(2)
	}
}

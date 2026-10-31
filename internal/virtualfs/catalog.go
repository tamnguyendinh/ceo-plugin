package virtualfs

import (
	"fmt"
	"sort"
)

var PrimaryRoles = []string{"ceo", "system-architect", "architect", "coder", "qa", "supervisor", "mechanical-planner", "strategic-planner"}

type Command struct{ Lane, Reference, URI, Description string }

// BuildCatalog derives inventory and command routing from the tree, while
// requiring deliberately maintained descriptions for every embedded asset.
func BuildCatalog(tree *VirtualAssetTree, descriptions map[string]string) ([]Command, error) {
	if tree == nil {
		return nil, fmt.Errorf("nil asset tree")
	}
	seen := map[string]bool{}
	for _, entry := range tree.ListEntries() {
		if descriptions[entry.Path] == "" {
			return nil, fmt.Errorf("missing catalog description: %s", entry.Path)
		}
		seen[entry.Path] = true
	}
	for path := range descriptions {
		if !seen[path] {
			return nil, fmt.Errorf("orphan catalog description: %s", path)
		}
	}
	var rows []Command
	coveredSkills := map[string]bool{}
	for _, role := range PrimaryRoles {
		entry, err := tree.GetSkill(role)
		if err != nil {
			return nil, err
		}
		rows = append(rows, Command{Lane: role, URI: entry.URI, Description: "Load the " + role + " SKILL instructions"})
		coveredSkills[entry.URI] = true
	}
	var children []Command
	for _, entry := range tree.ListEntries() {
		if entry.Kind == KindSkill {
			if !coveredSkills[entry.URI] {
				return nil, fmt.Errorf("skill has no public role descriptor: %s", entry.Path)
			}
			continue
		}
		lane := entry.Role
		if lane == "planner" {
			lane = "strategic-planner"
		}
		ref := entry.Name
		if entry.Kind == KindTemplate {
			ref = entry.NormalizedName
		}
		resolved, err := tree.GetReference(lane, ref)
		if err != nil || resolved != entry {
			return nil, fmt.Errorf("catalog command cannot resolve %s", entry.Path)
		}
		children = append(children, Command{lane, ref, entry.URI, descriptions[entry.Path]})
	}
	order := map[string]int{"ceo": 0, "system-architect": 1, "qa": 2, "supervisor": 3, "strategic-planner": 4}
	sort.Slice(children, func(i, j int) bool {
		a, b := children[i], children[j]
		if a.Lane != b.Lane {
			return order[a.Lane] < order[b.Lane]
		}
		return a.Reference < b.Reference
	})
	return append(rows, children...), nil
}

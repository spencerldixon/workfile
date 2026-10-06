package workfile

import (
	"fmt"
	"go.yaml.in/yaml/v3"
)

// Requires accepts either shared gates or a map of destination-specific gates.
// Custom decoding retains the strict field and duplicate-key checks used by
// the rest of the policy loader.
func (t *Transition) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: transition must be a mapping", node.Line)
	}
	var decoded Transition
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Tag != "!!str" || seen[key.Value] {
			return fmt.Errorf("line %d: duplicate or invalid transition field", key.Line)
		}
		seen[key.Value] = true
		var target any
		switch key.Value {
		case "to":
			target = &decoded.To
		case "requires":
			if value.Kind == yaml.MappingNode {
				for j := 0; j < len(value.Content); j += 2 {
					destination, gates := value.Content[j], value.Content[j+1]
					if destination.Tag != "!!str" || gates.Kind != yaml.SequenceNode {
						return fmt.Errorf("line %d: destination requirements must be gate lists", destination.Line)
					}
				}
				target = &decoded.RequiresByDestination
			} else {
				target = &decoded.Requires
			}
		case "routes":
			target = &decoded.Routes
		case "end":
			target = &decoded.End
		default:
			return fmt.Errorf("line %d: unknown transition field", key.Line)
		}
		if err := value.Decode(target); err != nil {
			return err
		}
	}
	*t = decoded
	return nil
}

package persistence

import (
	"strings"
	"unicode/utf8"
)

// PromptPart is an editable draft, not a provider input. Missing source nodes
// remain valid here so a deleted reference can be displayed and repaired.
type PromptPart struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	NodeID string `json:"nodeId,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Name   string `json:"name,omitempty"`
}

func (n NodeData) HasPromptReferences() bool {
	for _, part := range n.PromptParts {
		if part.Type == "reference" {
			return true
		}
	}
	return false
}

func validPromptParts(n Node) bool {
	parts := n.Data.PromptParts
	if len(parts) == 0 {
		return true
	}
	if len(parts) > 1000 || n.Data.Kind == "audio" {
		return false
	}
	var text strings.Builder
	for _, part := range parts {
		switch part.Type {
		case "text":
			if part.NodeID != "" || part.Kind != "" || part.Name != "" {
				return false
			}
			text.WriteString(part.Text)
		case "reference":
			if !ValidID(part.NodeID) || part.NodeID == n.ID || part.Text != "" || (part.Kind != "image" && part.Kind != "video") || (n.Data.Kind == "image" && part.Kind != "image") || strings.TrimSpace(part.Name) == "" || utf8.RuneCountInString(part.Name) > 255 {
				return false
			}
			text.WriteString("@" + part.Name)
		default:
			return false
		}
	}
	return text.String() == n.Data.Prompt && utf8.RuneCountInString(text.String()) <= 10000
}

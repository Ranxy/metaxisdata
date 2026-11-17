package agent

import "strings"

// PromptSection is one named block of a system prompt. Sections are composed by a
// product spec so its identity, rules and runtime guidance stay separable.
type PromptSection struct {
	// Name identifies the section for a reader and for tests.
	Name string
	// Text is the section body.
	Text string
}

// Prompt is an ordered system prompt assembled from named sections.
type Prompt struct {
	sections []PromptSection
}

// NewPrompt builds a prompt from its sections, in order.
func NewPrompt(sections ...PromptSection) *Prompt {
	return &Prompt{sections: sections}
}

// Sections returns the prompt's sections in order.
func (p *Prompt) Sections() []PromptSection {
	if p == nil {
		return nil
	}
	return p.sections
}

// Render joins the non-empty section bodies. The join is the contract: adding a
// section must not silently change the prompt an unchanged spec renders.
func (p *Prompt) Render() string {
	if p == nil {
		return ""
	}
	texts := make([]string, 0, len(p.sections))
	for _, section := range p.sections {
		if section.Text != "" {
			texts = append(texts, section.Text)
		}
	}
	return strings.Join(texts, "\n")
}

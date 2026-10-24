// Package prompt provides built-in prompt templates users can apply in chat.
package prompt

// Template is a reusable prompt template.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Content     string `json:"content"`
}

// Templates is the built-in library of prompt templates.
var Templates = []Template{
	{
		ID:          "code-review",
		Name:        "Code Review",
		Description: "Review code for bugs, security issues, and style improvements",
		Category:    "code",
		Content: `You are a senior code reviewer. Review the following code for bugs, security issues, performance problems, and style improvements. Provide specific, actionable feedback with line references where possible.

Code to review:
{{input}}`,
	},
	{
		ID:          "explain",
		Name:        "Explain Concept",
		Description: "Explain a complex topic in simple terms",
		Category:    "learning",
		Content: `You are an expert teacher who excels at explaining complex topics in simple, easy-to-understand terms. Use analogies, examples, and avoid unnecessary jargon.

Topic to explain:
{{input}}`,
	},
	{
		ID:          "summarize",
		Name:        "Summarize",
		Description: "Summarize text into concise bullet points",
		Category:    "writing",
		Content: `Please summarize the following text into concise bullet points. Capture the key points, main arguments, and any conclusions or action items.

Text to summarize:
{{input}}`,
	},
	{
		ID:          "translate",
		Name:        "Translate",
		Description: "Translate text between languages",
		Category:    "language",
		Content: `You are a professional translator. Translate the following text accurately, preserving tone, nuance, and formatting. Do not add extra commentary.

Text to translate:
{{input}}`,
	},
	{
		ID:          "brainstorm",
		Name:        "Brainstorm Ideas",
		Description: "Generate creative ideas on a topic",
		Category:    "creative",
		Content: `You are a creative brainstorming partner. Generate 10 original, practical ideas related to the topic below. For each idea, include a brief description and why it might work.

Topic:
{{input}}`,
	},
	{
		ID:          "critique",
		Name:        "Critique Writing",
		Description: "Critique writing for clarity and impact",
		Category:    "writing",
		Content: `You are an experienced editor. Critique the following writing for clarity, impact, structure, and tone. Suggest specific improvements.

Writing to critique:
{{input}}`,
	},
	{
		ID:          "debate",
		Name:        "Debate Topic",
		Description: "Argue both sides of a controversial topic",
		Category:    "learning",
		Content: `You are a skilled debater. Present strong arguments for both sides of the following topic, then provide a balanced conclusion. Be fair and thorough.

Topic:
{{input}}`,
	},
	{
		ID:          "rewrite",
		Name:        "Rewrite Text",
		Description: "Rewrite text for a different tone or audience",
		Category:    "writing",
		Content: `You are a skilled writer. Rewrite the following text to match the specified tone and audience while preserving the original meaning. Explain what you changed and why.

Text to rewrite:
{{input}}

Target tone/audience:
{{tone}}`,
	},
}

// Find returns the template with the given ID, or nil if not found.
func Find(id string) *Template {
	for i := range Templates {
		if Templates[i].ID == id {
			return &Templates[i]
		}
	}
	return nil
}

// Categories returns the unique categories sorted alphabetically.
func Categories() []string {
	seen := map[string]bool{}
	var cats []string
	for _, t := range Templates {
		if !seen[t.Category] {
			seen[t.Category] = true
			cats = append(cats, t.Category)
		}
	}
	// Simple sort
	for i := 0; i < len(cats); i++ {
		for j := i + 1; j < len(cats); j++ {
			if cats[i] > cats[j] {
				cats[i], cats[j] = cats[j], cats[i]
			}
		}
	}
	return cats
}

// FilterByCategory returns templates in the given category, or all if empty.
func FilterByCategory(category string) []Template {
	if category == "" {
		return Templates
	}
	var out []Template
	for _, t := range Templates {
		if t.Category == category {
			out = append(out, t)
		}
	}
	return out
}

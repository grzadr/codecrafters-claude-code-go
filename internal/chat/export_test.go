package chat

// ReadSkills exports the unexported readSkills function for testing.
func ReadSkills(path string) []Skill {
	return readSkills(path)
}

// ParsePrompt exports the unexported parsePrompt function for testing.
func ParsePrompt(prompt string, skills []Skill) string {
	return parsePrompt(prompt, skills)
}

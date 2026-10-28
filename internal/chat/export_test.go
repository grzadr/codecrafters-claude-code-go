package chat

// ReadSkills exports the unexported readSkills function for testing.
func ReadSkills(path string) SkillRegistry {
	return readSkills(path)
}

// ParsePrompt exports the unexported parsePrompt function for testing.
func ParsePrompt(prompt string, skills SkillRegistry) []string {
	return parsePrompt(prompt, skills)
}

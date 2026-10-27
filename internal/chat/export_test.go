package chat

// ReadSkills exports the unexported readSkills function for testing.
func ReadSkills(path string) []Skill {
	return readSkills(path)
}

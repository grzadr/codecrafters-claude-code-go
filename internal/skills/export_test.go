package skills

import "sync"

// ReadSkills exports the unexported readSkills function for testing.
func ReadSkills(path string) SkillRegistry {
	return readSkills(path)
}

// ResetSkills clears the GetSkills cache so each test can load its own skills dir.
func ResetSkills() {
	registry = nil
	once = sync.Once{}
}

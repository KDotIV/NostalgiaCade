package processenv

import "strings"

// Cabinet keeps the administrator's connection string out of child cores and
// third-party emulators when development and administration share a Windows user.
func Cabinet(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "NCADE_POSTGRES_DSN") || strings.EqualFold(name, "NCADE_TEST_POSTGRES_DSN") {
			continue
		}
		result = append(result, entry)
	}
	return result
}

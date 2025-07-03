package util

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	repoNameMaxLen  = 63
	repoNameHashLen = 8
)

// repo name may contain between 3 and 63 lowercase letters, digits and hyphens.
// sanitizeRepoName replaces all slashes with hyphens, and truncate the name.
func SanitizeRepoName(repoPrefix, name string) string {
	fullName := "cs-e2e-" + repoPrefix + "-" + name
	hashBytes := sha1.Sum([]byte(fullName))
	hashStr := hex.EncodeToString(hashBytes[:])[:repoNameHashLen]

	if len(fullName) > repoNameMaxLen-1-repoNameHashLen {
		fullName = fullName[:repoNameMaxLen-1-repoNameHashLen]
	}
	return fmt.Sprintf("%s-%s", strings.ReplaceAll(fullName, "/", "-"), hashStr)
}

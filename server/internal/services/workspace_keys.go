package services

import "strings"

// workspaceKeyMax matches the size:191 indexed workspace columns, the longest
// utf8mb4 key MySQL can index.
const workspaceKeyMax = 191

func validWorkspaceKey(value string) bool {
	return value != "" && len(value) <= workspaceKeyMax && !strings.ContainsRune(value, 0)
}

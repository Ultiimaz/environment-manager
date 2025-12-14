package sync

import "strings"

// Commit message prefixes for different types of commits
const (
	StateSnapshotPrefix = "[state-snapshot]"
	BackupPrefix        = "[backup]"
)

// CommitType represents the type of a commit
type CommitType int

const (
	// CommitTypeConfig is a configuration change that should trigger reconciliation
	CommitTypeConfig CommitType = iota
	// CommitTypeStateSnapshot is an automated state snapshot that should NOT trigger reconciliation
	CommitTypeStateSnapshot
	// CommitTypeBackup is a backup commit that should NOT trigger reconciliation
	CommitTypeBackup
)

// ClassifyCommit determines the type of commit based on its message
func ClassifyCommit(message string) CommitType {
	message = strings.TrimSpace(message)

	if strings.HasPrefix(message, StateSnapshotPrefix) {
		return CommitTypeStateSnapshot
	}

	if strings.HasPrefix(message, BackupPrefix) {
		return CommitTypeBackup
	}

	return CommitTypeConfig
}

// ShouldTriggerReconcile returns whether a commit type should trigger reconciliation
func ShouldTriggerReconcile(commitType CommitType) bool {
	return commitType == CommitTypeConfig
}

// IsStateSnapshotCommit checks if a commit message indicates a state snapshot
func IsStateSnapshotCommit(message string) bool {
	return ClassifyCommit(message) == CommitTypeStateSnapshot
}

// IsBackupCommit checks if a commit message indicates a backup commit
func IsBackupCommit(message string) bool {
	return ClassifyCommit(message) == CommitTypeBackup
}

// ShouldSkipReconcile checks if a commit should skip reconciliation
func ShouldSkipReconcile(message string) bool {
	commitType := ClassifyCommit(message)
	return commitType == CommitTypeStateSnapshot || commitType == CommitTypeBackup
}

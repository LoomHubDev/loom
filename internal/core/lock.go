package core

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// VaultLock provides file-based locking for concurrent vault access across processes.
type VaultLock struct {
	file *os.File
	path string
}

// AcquireLock acquires an exclusive lock on the vault.
// Returns a VaultLock that must be released with Release().
func AcquireLock(loomPath string) (*VaultLock, error) {
	lockPath := filepath.Join(loomPath, "locks", "vault.lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	// Try non-blocking exclusive lock
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("vault is locked by another process")
	}

	// Write PID for debugging
	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "%d\n", os.Getpid())

	return &VaultLock{file: f, path: lockPath}, nil
}

// Release releases the vault lock.
func (l *VaultLock) Release() error {
	if l.file == nil {
		return nil
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	l.file = nil
	return nil
}

package lock

import "io"

// Lock represents the lock state of a system.
// It allows:
//   - getting/setting the locked state
//   - being notified of changes to the locked state
//   - being notified of lock signals
//   - being notified of unlock signals
//
// It is safe to call Lock's methods concurrently.
type Lock interface {

	// GetLocked gets the current state of the system; true=Locked, false=unlocked.
	GetLocked() (bool, error)

	// SetLocked sets the current state of the system; true=Locked, false=unlocked.
	SetLocked(locked bool) error

	// AddLockSignal registers a channel that will be notified when the "Lock" signal is received.
	// Receiving this means that the system should be locked.
	// After locking, SetLocked(true) should be used.
	//
	// Writing to this channel does not block.
	// Use a buffered channel if you don't want to miss anything.
	AddLockSignal(c chan<- struct{}) error

	// RemoveLockSignal unregisters a channel previously registered with AddLockSignal.
	// RemoveLockSignal can be safely called with an unregistered channel.
	RemoveLockSignal(c chan<- struct{}) error

	// AddUnlockSignal registers a channel that will be notified when the "Unlock" signal is
	// received.
	// Receiving this means that the system should be unlocked.
	// After locking, SetLocked(false) should be used.
	//
	// Writing to this channel does not block.
	// Use a buffered channel if you don't want to miss anything.
	AddUnlockSignal(c chan<- struct{}) error

	// RemoveUnlockSignal unregisters a channel previously registered with AddUnlockSignal.
	// RemoveUnlockSignal can be safely called with an unregistered channel.
	RemoveUnlockSignal(c chan<- struct{}) error

	// AddLockedSignal registers a channel that will be notified when the system is locked (true)
	// or unlocked (false).
	// Writing to this channel does not block.
	// Use a buffered channel if you don't want to miss anything.
	AddLockedSignal(c chan<- bool) error

	// RemoveLockedSignal unregisters a channel previously registered with AddLockedSignal.
	// RemoveLockedSignal can be safely called with an unregistered channel.
	RemoveLockedSignal(c chan<- bool) error
	io.Closer
}

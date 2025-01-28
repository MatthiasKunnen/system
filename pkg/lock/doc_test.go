package lock_test

import (
	"github.com/MatthiasKunnen/system/pkg/lock"
	"log"
	"os"
	"time"
)

func ExampleLock_dbus() {
	l, err := lock.NewDbusSessionLock(os.Getenv("XDG_SESSION_ID"))
	if err != nil {
		log.Fatalf("Failed to initialize dbus lock: %v", err)
	}

	lockSignal := make(chan struct{}, 1)
	unlockSignal := make(chan struct{}, 1)
	lockedSignal := make(chan bool, 1)

	err = l.AddLockSignal(lockSignal)
	if err != nil {
		log.Fatalf("Failed to add lock signal: %v", err)
	}

	err = l.AddUnlockSignal(unlockSignal)
	if err != nil {
		log.Fatalf("Failed to add unlock signal: %v", err)
	}

	err = l.AddLockedSignal(lockedSignal)
	if err != nil {
		log.Fatalf("Failed to add locked signal: %v", err)
	}

	stop := time.After(10 * time.Second)
	for {
		select {
		case <-lockSignal:
			log.Println("Lock signal received, lock the system")
			// Write code to lock the system
			err := l.SetLocked(true)
			if err != nil {
				log.Printf("Failed to set locked to true: %v", err)
			}
		case <-unlockSignal:
			log.Println("Unlock signal received, unlock the system")
			// Write code that unlocks the system
			err := l.SetLocked(false)
			if err != nil {
				log.Printf("Failed to set locked to true: %v", err)
			}
		case locked := <-lockedSignal:
			if locked {
				log.Println("The system is now locked")
			} else {
				log.Println("The system is now unlocked")
			}
			// Write code that should do something based on lock state change
		case <-stop:
			err := l.Close()
			if err != nil {
				log.Printf("Failed to close dbus: %v", err)
			}
			return
		}
	}
}

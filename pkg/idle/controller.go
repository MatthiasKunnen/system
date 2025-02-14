package idle

import (
	"time"
)

type Controller interface {
	AddNotification(notificationInput *CreateIdleNotification) (Notification, error)
	// Close closes any connection the Controller might have. Do not use the Controller after
	// this.
	Close() error
}

type Notification interface {
	// Close destroys this notification.
	// Safe to be called from another goroutine.
	Close() error
}

type CreateIdleNotification struct {
	Duration time.Duration

	// Idle is the channel that will be notified when the system has idled.
	Idle chan<- struct{}

	// Resume is the channel that will be notified when the system has resumed.
	Resume chan<- struct{}
}

package idle

import (
	"errors"
	"fmt"
	"github.com/MatthiasKunnen/go-wayland/wayland/client"
	idleNotify "github.com/MatthiasKunnen/go-wayland/wayland/staging/ext-idle-notify-v1"
	"math"
)

type waylandIdleController struct {
	close chan struct{}
	// The dispatch channel exists to synchronize the wayland communication which is not safe to be
	// done over multiple goroutines.
	dispatchChan chan func() error
	display      *client.Display
	notifier     *idleNotify.IdleNotifier
	registry     *client.Registry
	seat         *client.Seat
}

type waylandIdleNotification struct {
	closed       bool
	controller   *waylandIdleController
	notification *idleNotify.IdleNotification
}

func (n *waylandIdleNotification) Close() error {
	if n.closed {
		return nil
	}
	n.closed = true

	go func() {
		closeFunc := func() error {
			// Destroy must be done in the same goroutine as dispatch and other
			// Wayland interactions.
			err := n.notification.Destroy()
			if err != nil {
				return fmt.Errorf("failed to close wayland idle notification: %w", err)
			}

			return nil
		}

		select {
		case <-n.controller.close:
		case n.controller.dispatchChan <- closeFunc:
		}
	}()

	return nil
}

// NewWaylandIdleController sets up a new Wayland connection.
// It returns:
//   - The controller
//   - The dispatch channel, execute the functions received on this channel on the same goroutine as
//     other interactions with the Controller.
//   - Error that occurred when creating the controller.
func NewWaylandIdleController() (Controller, <-chan func() error, error) {
	m := &waylandIdleController{
		close:        make(chan struct{}, 1),
		dispatchChan: make(chan func() error),
	}
	var err error
	m.display, err = client.Connect("")
	if err != nil {
		return nil, nil, fmt.Errorf("error connecting to Wayland server: %w", err)
	}

	m.registry, err = m.display.GetRegistry()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting Wayland registry: %w", err)
	}

	var globalHandlerError error
	m.registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		switch e.Interface {
		case idleNotify.IdleNotifierInterfaceName:
			m.notifier = idleNotify.NewIdleNotifier(m.context())
			err := m.registry.Bind(e.Name, idleNotify.IdleNotifierInterfaceName, e.Version, m.notifier)
			if err != nil {
				globalHandlerError = errors.Join(
					globalHandlerError,
					fmt.Errorf("unable to bind %s interface: %v", idleNotify.IdleNotifierInterfaceName, err),
				)
			}
		case client.SeatInterfaceName:
			seat := client.NewSeat(m.context())
			err := m.registry.Bind(e.Name, e.Interface, e.Version, seat)
			if err != nil {
				globalHandlerError = errors.Join(
					globalHandlerError,
					fmt.Errorf("unable to bind %s interface: %v", client.SeatInterfaceName, err),
				)
			}
			m.seat = seat
		}
	})

	err = m.display.Roundtrip()
	if err != nil {
		return nil, nil, fmt.Errorf("failed roundtrip one: %v", err)
	}
	if globalHandlerError != nil {
		return nil, nil, fmt.Errorf("error in registry GlobalHandler after roundtrip one: %w", globalHandlerError)
	}
	err = m.display.Roundtrip()
	if err != nil {
		return nil, nil, fmt.Errorf("failed roundtrip two: %v", err)
	}
	if globalHandlerError != nil {
		return nil, nil, fmt.Errorf("error in registry GlobalHandler after roundtrip two: %w", globalHandlerError)
	}

	if m.notifier == nil {
		err := m.Close()
		if err != nil {
			log.Printf("error closing WaylandIdleController: %v", err)
		}
		return nil, nil, errors.New("no notifier was set, ext-idle-notify might not be supported")
	}

	go func() {
		for {
			select {
			case m.dispatchChan <- m.display.Context().GetDispatch():
			case <-m.close:
				return
			}
		}
	}()

	return m, m.dispatchChan, nil
}

func (m *waylandIdleController) context() *client.Context {
	return m.display.Context()
}

func (m *waylandIdleController) Close() error {
	var totalError error
	if m.seat != nil {
		if err := m.seat.Release(); err != nil {
			totalError = errors.Join(totalError, fmt.Errorf("error releasing seat: %w", err))
		}
	}

	if m.display != nil {
		err := m.display.Destroy()
		if err != nil {
			totalError = errors.Join(totalError, fmt.Errorf("error destroying display: %w", err))
		}
	}
	if m.notifier != nil {
		if err := m.notifier.Destroy(); err != nil {
			totalError = errors.Join(totalError, fmt.Errorf(
				"unable to destroy %s: %w",
				idleNotify.IdleNotifierInterfaceName,
				err,
			))
		}
	}

	close(m.close)

	if err := m.context().Close(); err != nil {
		totalError = errors.Join(totalError, fmt.Errorf("error closing wayland connection: %w", err))
	}

	return totalError
}

// AddNotification registers notification handlers on idle and resume.
// idleEvent will be called when after the session is idle for the given duration.
// resumeEvent will be called when the session is active again after being idle for the given
// duration.
// One of idleEvent or resumeEvent must be non-nil.
func (m *waylandIdleController) AddNotification(notificationInput *CreateIdleNotification) (Notification, error) {
	if notificationInput.Idle == nil && notificationInput.Resume == nil {
		return nil, fmt.Errorf("either Idle or Resume is required")
	}

	durationMs := notificationInput.Duration.Milliseconds()
	switch {
	case durationMs > math.MaxUint32:
		return nil, fmt.Errorf("duration too large, %d > %d", durationMs, math.MaxUint32)
	case durationMs < 0:
		durationMs = 0
	}

	notification, err := m.notifier.GetIdleNotification(uint32(durationMs), m.seat)
	if err != nil {
		return nil, fmt.Errorf("unable to get idle notification: %w", err)
	}

	if notificationInput.Idle != nil {
		notification.SetIdledHandler(func(event idleNotify.IdleNotificationIdledEvent) {
			go func() {
				// Execute in goroutine to prevent blocking dispatch
				select {
				case notificationInput.Idle <- struct{}{}:
				case <-m.close:
				}
			}()
		})
	}

	if notificationInput.Resume != nil {
		notification.SetResumedHandler(func(event idleNotify.IdleNotificationResumedEvent) {
			go func() {
				// Execute in goroutine to prevent blocking dispatch
				select {
				case notificationInput.Resume <- struct{}{}:
				case <-m.close:
				}
			}()
		})
	}

	return &waylandIdleNotification{
		controller:   m,
		notification: notification,
	}, nil
}

package lock

import (
	"errors"
	"fmt"
	"github.com/godbus/dbus/v5"
	"sync"
)

type dbusCon struct {
	conn               *dbus.Conn
	loginSessionObject dbus.BusObject
	muSignals          sync.Mutex
	closeSignalHandler chan struct{}

	lockSignals       map[chan<- struct{}]struct{}
	lockedHintSignals map[chan<- bool]struct{}
	unlockSignals     map[chan<- struct{}]struct{}

	lockSignalActive        bool
	propertiesChangedActive bool
	unlockSignalActive      bool
}

// NewDbusSessionLock creates and initializes a D-Bus [org.freedesktop.login1] implementation of the
// Lock interface for the given session.
//
// sessionId is the ID of the session. Usually set to the XDG_SESSION_ID env var.
//
// [org.freedesktop.login1]: https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.login1.html
func NewDbusSessionLock(sessionId string) (Lock, error) {
	if sessionId == "" {
		return nil, errors.New("sessionId is empty")
	}

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}
	result := &dbusCon{
		conn:                    conn,
		loginSessionObject:      conn.Object("org.freedesktop.login1", "/org/freedesktop/login1"),
		lockSignals:             make(map[chan<- struct{}]struct{}),
		lockSignalActive:        false,
		unlockSignals:           make(map[chan<- struct{}]struct{}),
		unlockSignalActive:      false,
		lockedHintSignals:       make(map[chan<- bool]struct{}),
		propertiesChangedActive: false,
		closeSignalHandler:      make(chan struct{}),
	}
	var sessions []interface{}
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		Call("org.freedesktop.login1.Manager.ListSessions", 0).
		Store(&sessions)
	if err != nil {
		return nil, err
	}

	for i, sessionInt := range sessions {
		session, ok := sessionInt.([]interface{})
		if !ok {
			return nil, fmt.Errorf("session %d is not an array of interface: %+v", i, session)
		}
		currentSessionId, ok := session[0].(string)

		if !ok {
			return nil, fmt.Errorf("session %d[0] is not a string: %+v", i, session[0])
		}

		if currentSessionId == sessionId {
			sessionPath, ok := session[4].(dbus.ObjectPath)
			if !ok {
				return nil, fmt.Errorf("session %d[4] is not an ObjectPath: %+v", i, session[4])
			}

			result.loginSessionObject = conn.Object("org.freedesktop.login1", sessionPath)
			break
		}
	}

	if result.loginSessionObject == nil {
		return nil, fmt.Errorf("failed to find session object")
	}

	c := make(chan *dbus.Signal)
	conn.Signal(c)
	go func() {
		for {
			select {
			case <-result.closeSignalHandler:
				conn.RemoveSignal(c)
			case v := <-c:
				result.handleIncomingSignal(v)
			}
		}
	}()

	return result, nil
}

func (dc *dbusCon) SetLocked(locked bool) error {
	err := dc.loginSessionObject.
		Call("org.freedesktop.login1.Session.SetLockedHint", 0, locked).Err
	if err != nil {
		return fmt.Errorf("could not set locked hint: %w", err)
	}

	return nil
}

func (dc *dbusCon) GetLocked() (bool, error) {
	variant, err := dc.loginSessionObject.GetProperty("org.freedesktop.login1.Session.LockedHint")
	if err != nil {
		return false, fmt.Errorf("could not get locked hint: %w", err)
	}

	lockedHint, ok := variant.Value().(bool)
	if !ok {
		return false, fmt.Errorf("LockedHint property result is not a boolean")
	}

	return lockedHint, nil
}

func (dc *dbusCon) AddLockSignal(c chan<- struct{}) error {
	if c == nil {
		return errors.New("AddLockSignal: channel cannot be nil")
	}

	dc.muSignals.Lock()
	defer dc.muSignals.Unlock()
	dc.lockSignals[c] = struct{}{}

	if !dc.lockSignalActive {
		if err := dc.conn.AddMatchSignal(
			dbus.WithMatchObjectPath(dc.loginSessionObject.Path()),
			dbus.WithMatchInterface("org.freedesktop.login1.Session"),
			dbus.WithMatchSender("org.freedesktop.login1"),
			dbus.WithMatchMember("Lock"),
		); err != nil {
			return fmt.Errorf("failed to register Dbus Lock signal: %w", err)
		}

		dc.lockSignalActive = true
	}

	return nil
}

func (dc *dbusCon) RemoveLockSignal(c chan<- struct{}) error {
	if c == nil {
		return errors.New("RemoveLockSignal: channel cannot be nil")
	}

	dc.muSignals.Lock()
	defer dc.muSignals.Unlock()

	delete(dc.lockSignals, c)

	if len(dc.lockSignals) == 0 {
		if err := dc.removeLockSignal(); err != nil {
			return err
		}
	}

	return nil
}

// removeLockSignal removes the Lock signal.
// Holding the muSignals mutex is required.
func (dc *dbusCon) removeLockSignal() error {
	if !dc.lockSignalActive {
		return nil
	}

	if err := dc.conn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(dc.loginSessionObject.Path()),
		dbus.WithMatchInterface("org.freedesktop.login1.Session"),
		dbus.WithMatchSender("org.freedesktop.login1"),
		dbus.WithMatchMember("Lock"),
	); err != nil {
		return fmt.Errorf("failed to remove Dbus Lock signal: %w", err)
	}

	dc.lockSignalActive = false

	return nil
}

func (dc *dbusCon) AddUnlockSignal(c chan<- struct{}) error {
	if c == nil {
		return errors.New("AddUnlockSignal: channel cannot be nil")
	}

	dc.muSignals.Lock()
	defer dc.muSignals.Unlock()
	dc.unlockSignals[c] = struct{}{}

	if !dc.unlockSignalActive {
		if err := dc.conn.AddMatchSignal(
			dbus.WithMatchObjectPath(dc.loginSessionObject.Path()),
			dbus.WithMatchInterface("org.freedesktop.login1.Session"),
			dbus.WithMatchSender("org.freedesktop.login1"),
			dbus.WithMatchMember("Unlock"),
		); err != nil {
			return fmt.Errorf("failed to register Dbus Unlock signal: %w", err)
		}

		dc.unlockSignalActive = true
	}

	return nil
}

func (dc *dbusCon) RemoveUnlockSignal(c chan<- struct{}) error {
	if c == nil {
		return errors.New("RemoveUnlockSignal: channel cannot be nil")
	}

	dc.muSignals.Lock()
	defer dc.muSignals.Unlock()

	delete(dc.unlockSignals, c)

	if len(dc.unlockSignals) == 0 {
		if err := dc.removeUnlockSignal(); err != nil {
			return err
		}
	}

	return nil
}

// removeUnlockSignal Removes the Unlock signal if it was registered.
// Holding the muSignals mutex is required.
func (dc *dbusCon) removeUnlockSignal() error {
	if !dc.unlockSignalActive {
		return nil
	}

	if err := dc.conn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(dc.loginSessionObject.Path()),
		dbus.WithMatchInterface("org.freedesktop.login1.Session"),
		dbus.WithMatchSender("org.freedesktop.login1"),
		dbus.WithMatchMember("Unlock"),
	); err != nil {
		return fmt.Errorf("failed to remove Dbus Unlock signal: %w", err)
	}

	dc.unlockSignalActive = false

	return nil
}

func (dc *dbusCon) AddLockedSignal(c chan<- bool) error {
	if c == nil {
		return errors.New("AddLockedSignal: channel cannot be nil")
	}

	dc.muSignals.Lock()
	defer dc.muSignals.Unlock()
	dc.lockedHintSignals[c] = struct{}{}

	if !dc.propertiesChangedActive {
		if err := dc.conn.AddMatchSignal(
			dbus.WithMatchObjectPath(dc.loginSessionObject.Path()),
			dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
			dbus.WithMatchSender("org.freedesktop.login1"),
			dbus.WithMatchMember("PropertiesChanged"),
		); err != nil {
			return fmt.Errorf("failed to register Dbus signal for LockedHint: %w", err)
		}

		dc.propertiesChangedActive = true
	}

	return nil
}

func (dc *dbusCon) RemoveLockedSignal(c chan<- bool) error {
	if c == nil {
		return errors.New("RemoveLockedSignal: channel cannot be nil")
	}

	dc.muSignals.Lock()
	defer dc.muSignals.Unlock()

	delete(dc.lockedHintSignals, c)

	if len(dc.lockedHintSignals) == 0 {
		if err := dc.removePropertiesChangedSignal(); err != nil {
			return err
		}
	}

	return nil
}

// removePropertiesChangedSignal Removes the PropertiesChangedSignal if it was registered.
// Holding the muSignals mutex is required.
func (dc *dbusCon) removePropertiesChangedSignal() error {
	if !dc.propertiesChangedActive {
		return nil
	}

	if err := dc.conn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(dc.loginSessionObject.Path()),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchSender("org.freedesktop.login1"),
		dbus.WithMatchMember("PropertiesChanged"),
	); err != nil {
		return fmt.Errorf("failed to remove Dbus PropertiesChanged signal: %w", err)
	}

	dc.propertiesChangedActive = false

	return nil
}

func (dc *dbusCon) Close() error {
	dc.muSignals.Lock()
	defer dc.muSignals.Unlock()

	var err error

	clear(dc.lockSignals)
	err = errors.Join(err, dc.removeLockSignal())
	clear(dc.unlockSignals)
	err = errors.Join(err, dc.removeUnlockSignal())
	clear(dc.lockedHintSignals)
	err = errors.Join(err, dc.removePropertiesChangedSignal())

	close(dc.closeSignalHandler)
	return err
}

func (dc *dbusCon) handleIncomingSignal(s *dbus.Signal) {
	if s == nil {
		// Seems to happen on close
		return
	}

	if s.Path != dc.loginSessionObject.Path() {
		return
	}

	dc.muSignals.Lock()
	defer dc.muSignals.Unlock()

	switch s.Name {
	case "org.freedesktop.login1.Session.Lock":
		for c := range dc.lockSignals {
			select {
			case c <- struct{}{}:
			default:
			}
		}
	case "org.freedesktop.login1.Session.Unlock":
		for c := range dc.unlockSignals {
			select {
			case c <- struct{}{}:
			default:
			}
		}
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		changedProperties := s.Body[1].(map[string]dbus.Variant)
		lockedHintProperty, hasLockedHint := changedProperties["LockedHint"]
		if !hasLockedHint {
			return
		}

		isLocked, ok := lockedHintProperty.Value().(bool)
		if !ok {
			panic("PropertiesChanged signal's LockedHint is not a boolean")
		}

		for c := range dc.lockedHintSignals {
			select {
			case c <- isLocked:
			default:
			}
		}
	}
}

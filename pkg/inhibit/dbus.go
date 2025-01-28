package inhibit

import (
	"errors"
	"fmt"
	"github.com/godbus/dbus/v5"
	"io"
	"os"
	"strings"
	"sync"
)

const (
	dbusDest             = "org.freedesktop.login1"
	dbusManagerInterface = "org.freedesktop.login1.Manager"
	dbusPath             = "/org/freedesktop/login1"
)

type Inhibitor struct {
	conn                   *dbus.Conn
	login1                 dbus.BusObject
	muSignals              sync.Mutex
	closeSignalHandler     chan struct{}
	prepareForSleepSubs    map[chan<- bool]struct{}
	prepareForShutdownSubs map[chan<- bool]struct{}
}

func New() (*Inhibitor, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	inhibitor := &Inhibitor{
		conn:                   conn,
		login1:                 conn.Object(dbusDest, dbusPath),
		prepareForSleepSubs:    make(map[chan<- bool]struct{}),
		prepareForShutdownSubs: make(map[chan<- bool]struct{}),
	}

	c := make(chan *dbus.Signal)
	conn.Signal(c)
	go func() {
		for {
			select {
			case <-inhibitor.closeSignalHandler:
				conn.RemoveSignal(c)
			case v := <-c:
				inhibitor.handleIncomingSignal(v)
			}
		}
	}()

	return inhibitor, nil
}

type What string

const (
	WhatHandleHibernateKey What = "handle-hibernate-key"
	WhatHandleLidSwitch    What = "handle-lid-switch"
	WhatHandlePowerKey     What = "handle-power-key"
	WhatHandleSuspendKey   What = "handle-suspend-key"
	WhatIdle               What = "idle"
	WhatShutdown           What = "shutdown"
	WhatSleep              What = "sleep"
)

type Mode string

const (
	ModeBlock     = "block"
	ModeBlockWeak = "block-weak"
	ModeDelay     = "delay"
)

// Inhibit creates an inhibition lock. It takes four parameters: what, who, why,
// and mode.
//   - what is one or more of actions that should be inhibited.
//   - who should be a short human-readable string identifying the application taking the lock.
//   - why should be a short human-readable string identifying the reason why the lock is taken.
//   - mode determines whether the inhibition shall be considered mandatory ("block") or whether it
//     should just delay the operation to a certain maximum time ("delay"),
//     while "block-weak" will create an inhibitor that is automatically ignored in some
//     circumstances.
//
// The lock is released the moment when the returned object and all its duplicates are closed.
func (i *Inhibitor) Inhibit(who string, why string, mode Mode, what ...What) (io.Closer, error) {
	var fd dbus.UnixFD

	err := i.login1.
		Call(dbusManagerInterface+".Inhibit", 0, joinWhat(what), who, why, mode).
		Store(&fd)
	if err != nil {
		return nil, fmt.Errorf("failed to create inhibit lock: %w", err)
	}

	return os.NewFile(uintptr(fd), "inhibit"), nil
}

func (i *Inhibitor) handleIncomingSignal(s *dbus.Signal) {
	if s == nil {
		// Seems to happen on close
		return
	}

	if s.Path != i.login1.Path() {
		return
	}

	i.muSignals.Lock()
	defer i.muSignals.Unlock()

	switch s.Name {
	case "org.freedesktop.login1.Manager.PrepareForSleep":
		change, ok := s.Body[0].(bool)
		if !ok {
			panic(fmt.Sprint("org.freedesktop.login1.Manager.PrepareForSleep signal, " +
				"body[0] is not a boolean"))
		}

		for c := range i.prepareForSleepSubs {
			select {
			case c <- change:
			default:
			}
		}
	case "org.freedesktop.login1.Manager.PrepareForShutdown":
		change, ok := s.Body[0].(bool)
		if !ok {
			panic(fmt.Sprint("org.freedesktop.login1.Manager.PrepareForShutdown signal, " +
				"body[0] is not a boolean"))
		}
		for c := range i.prepareForShutdownSubs {
			select {
			case c <- change:
			default:
			}
		}
	}
}

// SubscribePrepareForSleep registers the channel so that it will be notified when the system wants
// to sleep (true) or resumes from suspend (false).
// Unregister the channel using UnsubscribePrepareForSleep.
func (i *Inhibitor) SubscribePrepareForSleep(c chan<- bool) error {
	if c == nil {
		return errors.New("SubscribePrepareForSleep: channel cannot be nil")
	}

	i.muSignals.Lock()
	defer i.muSignals.Unlock()

	if len(i.prepareForSleepSubs) == 0 {
		if err := i.conn.AddMatchSignal(
			dbus.WithMatchObjectPath(i.login1.Path()),
			dbus.WithMatchInterface(dbusManagerInterface),
			dbus.WithMatchSender(dbusDest),
			dbus.WithMatchMember("PrepareForSleep"),
		); err != nil {
			return fmt.Errorf("failed to register Dbus PrepareForSleep signal: %w", err)
		}
	}

	i.prepareForSleepSubs[c] = struct{}{}

	return nil
}

func (i *Inhibitor) UnsubscribePrepareForSleep(c chan<- bool) error {
	if c == nil {
		return errors.New("RemoveLockSignal: channel cannot be nil")
	}

	i.muSignals.Lock()
	defer i.muSignals.Unlock()

	delete(i.prepareForSleepSubs, c)

	if len(i.prepareForSleepSubs) == 0 {
		if err := i.removePrepareForSleepSignal(); err != nil {
			return err
		}
	}

	return nil
}

func (i *Inhibitor) removePrepareForSleepSignal() error {
	if err := i.conn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(i.login1.Path()),
		dbus.WithMatchInterface(dbusManagerInterface),
		dbus.WithMatchSender(dbusDest),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		return fmt.Errorf("failed to remove Dbus PrepareForSleep signal: %w", err)
	}

	return nil
}

// SubscribePrepareForShutdown registers the channel so that it will be notified when the system
// wants to shut down or reboot (true). False is not expected since all programs will have closed
// after restarting the system.
// Unregister the channel using UnsubscribePrepareForShutdown.
func (i *Inhibitor) SubscribePrepareForShutdown(c chan<- bool) error {
	if c == nil {
		return errors.New("SubscribePrepareForShutdown: channel cannot be nil")
	}

	i.muSignals.Lock()
	defer i.muSignals.Unlock()

	if len(i.prepareForShutdownSubs) == 0 {
		if err := i.conn.AddMatchSignal(
			dbus.WithMatchObjectPath(i.login1.Path()),
			dbus.WithMatchInterface(dbusManagerInterface),
			dbus.WithMatchSender(dbusDest),
			dbus.WithMatchMember("PrepareForShutdown"),
		); err != nil {
			return fmt.Errorf("failed to register Dbus PrepareForShutdown signal: %w", err)
		}
	}

	i.prepareForShutdownSubs[c] = struct{}{}

	return nil
}

func (i *Inhibitor) UnsubscribePrepareForShutdown(c chan<- bool) error {
	if c == nil {
		return errors.New("PrepareForShutdown: channel cannot be nil")
	}

	i.muSignals.Lock()
	defer i.muSignals.Unlock()

	delete(i.prepareForShutdownSubs, c)

	if len(i.prepareForShutdownSubs) == 0 {
		if err := i.removePrepareForShutdownSignal(); err != nil {
			return err
		}
	}

	return nil
}

func (i *Inhibitor) removePrepareForShutdownSignal() error {
	if err := i.conn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(i.login1.Path()),
		dbus.WithMatchInterface(dbusManagerInterface),
		dbus.WithMatchSender(dbusDest),
		dbus.WithMatchMember("PrepareForShutdown"),
	); err != nil {
		return fmt.Errorf("failed to remove Dbus PrepareForShutdown signal: %w", err)
	}

	return nil
}

// Close permanently stops processing signals. Discard the inhibitor afterward.
func (i *Inhibitor) Close() error {
	i.muSignals.Lock()
	defer i.muSignals.Unlock()

	var err error

	clear(i.prepareForSleepSubs)
	err = errors.Join(err, i.removePrepareForShutdownSignal())
	clear(i.prepareForShutdownSubs)
	err = errors.Join(err, i.removePrepareForSleepSignal())

	close(i.closeSignalHandler)
	return err
}

func joinWhat(elems []What) string {
	const sep = ":"
	var n int
	n += len(sep) * (len(elems) - 1)
	for _, elem := range elems {
		n += len(elem)
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(string(elems[0]))
	for _, s := range elems[1:] {
		b.WriteString(sep)
		b.WriteString(string(s))
	}
	return b.String()
}

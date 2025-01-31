package secrets

import (
	"fmt"
	"github.com/godbus/dbus/v5"
)

const (
	dbusDest             = "org.freedesktop.secrets"
	dbusServiceInterface = "org.freedesktop.Secret.Service"
	dbusPath             = "/org/freedesktop/secrets"
)

type Secrets struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

func New() (*Secrets, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}

	s := &Secrets{
		conn: conn,
	}
	s.obj = conn.Object(dbusDest, dbusPath)

	return s, nil
}

// Lock locks the given objects. The given objects are prepended by "/org/freedesktop/secrets/".
func (s *Secrets) Lock(paths []string) error {
	objs := make([]dbus.ObjectPath, len(paths), len(paths))
	for i, path := range paths {
		objs[i] = dbus.ObjectPath(dbusPath + "/" + path)
	}
	err := s.obj.Call(dbusServiceInterface+".Lock", 0, objs).Err
	if err != nil {
		return fmt.Errorf("could lock collection: %w", err)
	}

	return nil
}

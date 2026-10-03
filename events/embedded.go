package events

import (
	"errors"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

func StartEmbedded(storeDir string) (*natsserver.Server, error) {
	srv, err := natsserver.NewServer(&natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		JetStream: true,
		StoreDir:  storeDir,
		NoSigs:    true,
		NoLog:     true,
	})
	if err != nil {
		return nil, err
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		srv.Shutdown()
		return nil, errors.New("embedded nats did not become ready")
	}
	return srv, nil
}

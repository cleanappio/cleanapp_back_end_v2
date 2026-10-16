package server

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// serveUntilShutdown stops accepting new requests after the shutdown signal,
// then waits for active requests to finish. In particular, token disbursement
// must finish its transaction/receipt work before the process exits. A request
// is not cancelled or forcibly abandoned because an arbitrary drain timer ran
// out; the maintenance caller must allow time for its current work to finish.
func serveUntilShutdown(ctx context.Context, srv *http.Server, listener net.Listener) error {
	result := make(chan error, 1)
	go func() { result <- srv.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		if err := srv.Shutdown(context.Background()); err != nil {
			return err
		}
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

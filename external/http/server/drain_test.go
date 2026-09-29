package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type drainContextKey struct{}

func TestRequestValuesSurviveSignalAndDrainBeforeReturn(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "deadline"}[expire], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			root, cancel := context.WithCancel(context.WithValue(context.Background(), drainContextKey{}, "runtime"))
			defer cancel()
			started := make(chan context.Context, 1)
			release := make(chan struct{})
			finished := make(chan struct{})
			draining := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				result <- StartServerWith(&StartServerWithRequest{
					Addr: listener.Addr().String(), Context: root, GracefulShutdownTimeout: 100 * time.Millisecond,
					Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						started <- r.Context()
						select {
						case <-release:
							_, _ = w.Write([]byte("drained"))
						case <-r.Context().Done():
						}
						close(finished)
					}),
					ListenAndServe: func(server *http.Server) error {
						err := server.Serve(listener)
						if errors.Is(err, http.ErrServerClosed) {
							return nil
						}
						return err
					},
					Shutdown: func(server *http.Server, ctx context.Context) error {
						close(draining)
						if ctx.Value(drainContextKey{}) != "runtime" || ctx.Err() != nil {
							return errors.New("shutdown lost live runtime context")
						}
						return server.Shutdown(ctx)
					},
				})
			}()
			clientDone := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: 2 * time.Second}
				response, err := client.Get("http://" + listener.Addr().String())
				if response != nil {
					_ = response.Body.Close()
				}
				clientDone <- err
			}()
			var requestCtx context.Context
			select {
			case requestCtx = <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not start")
			}
			require.Equal(t, "runtime", requestCtx.Value(drainContextKey{}))
			cancel()
			select {
			case <-draining:
			case <-time.After(time.Second):
				t.Fatal("shutdown did not start")
			}
			require.NoError(t, requestCtx.Err(), "process cancellation must allow in-flight requests to drain")
			if !expire {
				close(release)
			}
			select {
			case err = <-result:
				if expire {
					require.ErrorIs(t, err, ErrShutdownFailure)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown did not return")
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("active request was not cancelled")
			}
			select {
			case err = <-clientDone:
				if expire {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("connection remained open")
			}
		})
	}
}

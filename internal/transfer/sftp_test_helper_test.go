package transfer

import (
	"io"
	"testing"

	"github.com/pkg/sftp"
)

// newInProcSFTPClient starts an in-process sftp server and returns a connected
// sftp.Client. The server operates on the real OS filesystem (tests use
// t.TempDir() paths). Both write-end pipes are closed first in cleanup so
// the client recv goroutine and the server Serve loop unblock before Close.
func newInProcSFTPClient(t *testing.T) *sftp.Client {
	t.Helper()

	// Two pipe pairs form a bidirectional channel:
	//   client writes → serverRd reads
	//   server writes → clientRd reads
	serverRd, clientWr := io.Pipe()
	clientRd, serverWr := io.Pipe()

	srv, err := sftp.NewServer(struct {
		io.Reader
		io.WriteCloser
	}{serverRd, serverWr})
	if err != nil {
		t.Fatalf("sftp.NewServer: %v", err)
	}

	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		_ = srv.Serve()
	}()

	client, err := sftp.NewClientPipe(clientRd, clientWr)
	if err != nil {
		_ = clientWr.Close()
		_ = serverWr.Close()
		t.Fatalf("sftp.NewClientPipe: %v", err)
	}

	t.Cleanup(func() {
		// Close both write ends first — this unblocks the client recv goroutine
		// and the server Serve loop so Close calls don't deadlock.
		_ = clientWr.Close()
		_ = serverWr.Close()
		_ = client.Close()
		<-srvDone
	})
	return client
}

package db_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"

	// Register the MySQL-wire drivers this test opens through an SSH tunnel.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/starrocks"
)

// testBastion is an in-process SSH server that counts the sessions it serves, so
// a test can tell whether a driver released the tunnel it opened.
type testBastion struct {
	host    string
	port    string
	hostKey ssh.PublicKey
	wg      sync.WaitGroup
	mu      sync.Mutex
	open    int
}

func startTestBastion(t *testing.T) *testBastion {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)

	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	bastion := &testBastion{host: host, port: port, hostKey: signer.PublicKey()}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			bastion.wg.Add(1)
			go bastion.serve(conn, config)
		}
	}()
	return bastion
}

func (b *testBastion) serve(conn net.Conn, config *ssh.ServerConfig) {
	defer b.wg.Done()
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		_ = conn.Close()
		return
	}
	b.mu.Lock()
	b.open++
	b.mu.Unlock()
	defer func() {
		_ = serverConn.Close()
		b.mu.Lock()
		b.open--
		b.mu.Unlock()
	}()

	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		_ = newChannel.Reject(ssh.UnknownChannelType, "this bastion only counts sessions")
	}
}

func (b *testBastion) sessions() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.open
}

func (b *testBastion) config(extra map[string]string) db.ConnectionConfig {
	return db.ConnectionConfig{
		DataSource: &storepb.DataSource{
			Host:                      "127.0.0.1",
			Port:                      "3306",
			Username:                  "root",
			SshHost:                   b.host,
			SshPort:                   b.port,
			SshHostKey:                ssh.FingerprintSHA256(b.hostKey),
			ExtraConnectionParameters: extra,
		},
		ConnectionContext: db.ConnectionContext{DatabaseName: "app"},
		Password:          "root",
	}
}

// Every path that opens a driver has to release the SSH tunnel it opened, and a
// driver that was never returned has no Close to call — so Open itself has to
// close the session when the DSN is rejected after the tunnel is up.
func TestDriverOpenReleasesTheSSHTunnel(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_STARROCKS} {
		t.Run(engine.String(), func(t *testing.T) {
			t.Parallel()

			bastion := startTestBastion(t)
			ctx := context.Background()

			// A data source the driver accepts keeps its tunnel until Close.
			driver, err := db.Open(ctx, engine, bastion.config(nil))
			require.NoError(t, err)
			require.Eventually(t, func() bool { return bastion.sessions() == 1 }, 5*time.Second, 10*time.Millisecond,
				"opening the driver opens the tunnel")
			require.NoError(t, driver.Close(ctx))
			require.Eventually(t, func() bool { return bastion.sessions() == 0 }, 5*time.Second, 10*time.Millisecond,
				"closing the driver closes the tunnel")

			// A parameter the MySQL-wire driver rejects fails after the tunnel
			// is up, and must not leave the session behind.
			_, err = db.Open(ctx, engine, bastion.config(map[string]string{"timeout": "zzz"}))
			require.Error(t, err)
			require.Eventually(t, func() bool { return bastion.sessions() == 0 }, 5*time.Second, 10*time.Millisecond,
				"a failed Open must release the tunnel")
		})
	}
}

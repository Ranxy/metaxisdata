package util

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// testSSHServer is an in-process SSH server that answers direct-tcpip channel
// requests, which is what a bastion does for the database drivers.
type testSSHServer struct {
	addr          string
	hostKey       ssh.PublicKey
	listener      net.Listener
	stallChannels bool
	mu            sync.Mutex
	clients       []*ssh.ServerConn
	wg            sync.WaitGroup
}

func startTestSSHServer(t *testing.T) *testSSHServer {
	t.Helper()
	return startSSHServer(t, nil, false)
}

// startStallingSSHServer completes the SSH handshake but never answers a channel
// request, which is what a host that stalls the database connection looks like.
func startStallingSSHServer(t *testing.T) *testSSHServer {
	t.Helper()
	return startSSHServer(t, nil, true)
}

// startTestSSHServerWithAuth starts a server with the given authentication
// config, so a test can force the client through a particular auth method.
func startTestSSHServerWithAuth(t *testing.T, config *ssh.ServerConfig) *testSSHServer {
	t.Helper()
	return startSSHServer(t, config, false)
}

func startSSHServer(t *testing.T, config *ssh.ServerConfig, stallChannels bool) *testSSHServer {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)

	if config == nil {
		config = &ssh.ServerConfig{NoClientAuth: true}
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &testSSHServer{
		addr:          listener.Addr().String(),
		hostKey:       signer.PublicKey(),
		listener:      listener,
		stallChannels: stallChannels,
	}
	go server.serve(config)
	t.Cleanup(server.close)
	return server
}

func (s *testSSHServer) serve(config *ssh.ServerConfig) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
			if err != nil {
				_ = conn.Close()
				return
			}
			s.mu.Lock()
			s.clients = append(s.clients, serverConn)
			s.mu.Unlock()
			go ssh.DiscardRequests(reqs)
			for newChannel := range chans {
				if s.stallChannels {
					// Drop the channel open request without answering it, so
					// the caller has to bound the open itself.
					continue
				}
				go forwardChannel(newChannel)
			}
		}()
	}
}

func forwardChannel(newChannel ssh.NewChannel) {
	if newChannel.ChannelType() != "direct-tcpip" {
		_ = newChannel.Reject(ssh.UnknownChannelType, "only direct-tcpip is supported")
		return
	}
	var payload struct {
		DestAddr   string
		DestPort   uint32
		OriginAddr string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(payload.DestAddr, strconv.Itoa(int(payload.DestPort))))
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	channel, channelReqs, err := newChannel.Accept()
	if err != nil {
		_ = target.Close()
		return
	}
	go ssh.DiscardRequests(channelReqs)
	go func() {
		defer channel.Close()
		defer target.Close()
		_, _ = io.Copy(target, channel)
	}()
	go func() {
		defer channel.Close()
		defer target.Close()
		_, _ = io.Copy(channel, target)
	}()
}

func (s *testSSHServer) close() {
	_ = s.listener.Close()
	s.mu.Lock()
	for _, conn := range s.clients {
		_ = conn.Close()
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// startSilentTCPServer accepts TCP connections and never speaks SSH, which is
// what a host that stalls the handshake looks like.
func startSilentTCPServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		for _, conn := range conns {
			_ = conn.Close()
		}
		mu.Unlock()
	})
	return listener.Addr().String()
}

func startEchoServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

func sshDataSource(addr, hostKey string) *storepb.DataSource {
	host, port, _ := net.SplitHostPort(addr)
	return &storepb.DataSource{SshHost: host, SshPort: port, SshHostKey: hostKey}
}

func newPublicKeyLine(t *testing.T) string {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)
	return string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
}

func TestGetSSHClientRefusesADataSourceWithoutAHostKey(t *testing.T) {
	t.Parallel()

	server := startTestSSHServer(t)
	client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, ""))
	require.ErrorContains(t, err, "ssh_host_key must list the trusted host key")
	require.Nil(t, client)
}

func TestGetSSHClientAcceptsTheConfiguredFingerprint(t *testing.T) {
	t.Parallel()

	server := startTestSSHServer(t)
	// ssh-keygen prints the fingerprint followed by the key's comment, and a
	// file may carry blank and comment lines; all of them must be tolerated.
	configured := "# bastion\n\n" + ssh.FingerprintSHA256(server.hostKey) + " root@bastion\n"
	client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, configured))
	require.NoError(t, err)
	require.NoError(t, client.Close())
}

func TestGetSSHClientAcceptsAnMD5Fingerprint(t *testing.T) {
	t.Parallel()

	server := startTestSSHServer(t)
	// Uppercase hex, as some tools print it. The library's MD5 helper returns
	// the bare digest, so the prefix comes from the test.
	configured := "MD5:" + strings.ToUpper(ssh.FingerprintLegacyMD5(server.hostKey))
	client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, configured))
	require.NoError(t, err)
	require.NoError(t, client.Close())
}

func TestGetSSHClientAcceptsAPublicKeyLine(t *testing.T) {
	t.Parallel()

	server := startTestSSHServer(t)
	for name, configured := range map[string]string{
		"authorized_keys": string(ssh.MarshalAuthorizedKey(server.hostKey)),
		"known_hosts":     knownhosts.Line([]string{knownhosts.Normalize(server.addr)}, server.hostKey),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, configured))
			require.NoError(t, err)
			require.NoError(t, client.Close())
		})
	}
}

func TestGetSSHClientRejectsAnUntrustedHostKey(t *testing.T) {
	t.Parallel()

	server := startTestSSHServer(t)
	_, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	otherSigner, err := ssh.NewSignerFromKey(otherPrivateKey)
	require.NoError(t, err)

	client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, ssh.FingerprintSHA256(otherSigner.PublicKey())))
	require.ErrorContains(t, err, "ssh host key mismatch")
	require.Nil(t, client)
}

func TestGetSSHClientRejectsAMalformedHostKey(t *testing.T) {
	t.Parallel()

	server := startTestSSHServer(t)
	for _, entry := range []string{"not-a-key", "SHA256:", "MD5:"} {
		client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, entry))
		require.ErrorContains(t, err, "invalid ssh_host_key", "entry %q", entry)
		require.Nil(t, client)
	}
}

func TestSSHHandshakeTimesOutAgainstASilentHost(t *testing.T) {
	t.Parallel()

	ds := sshDataSource(startSilentTCPServer(t), newPublicKeyLine(t))
	start := time.Now()
	client, err := getSSHClient(context.Background(), ds, 200*time.Millisecond)
	require.Error(t, err)
	require.Nil(t, client)
	require.Less(t, time.Since(start), 5*time.Second, "the handshake must not hang")
}

func TestSSHHandshakeHonorsTheContextDeadline(t *testing.T) {
	t.Parallel()

	ds := sshDataSource(startSilentTCPServer(t), newPublicKeyLine(t))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	client, err := getSSHClient(ctx, ds, time.Minute)
	require.Error(t, err)
	require.Nil(t, client)
	require.Less(t, time.Since(start), 5*time.Second, "the context deadline must bound the handshake")
}

func TestSSHHandshakeStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	ds := sshDataSource(startSilentTCPServer(t), newPublicKeyLine(t))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	client, err := getSSHClient(ctx, ds, time.Minute)
	require.Error(t, err)
	require.Nil(t, client)
	// The configured timeout is a minute, so returning quickly is only
	// explained by the cancellation closing the connection.
	require.Less(t, time.Since(start), 5*time.Second, "cancelling the context must interrupt the handshake")
}

// The agent socket is read during authentication, which the SSH connection's
// deadline does not cover; a wedged agent must not hang the handshake either.
func TestSSHHandshakeIsBoundedWhenTheAgentStalls(t *testing.T) {
	// t.Setenv forbids t.Parallel.
	agentSocket, err := net.Listen("unix", filepath.Join(t.TempDir(), "agent.sock"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = agentSocket.Close() })
	go func() {
		for {
			conn, err := agentSocket.Accept()
			if err != nil {
				return
			}
			// Hold the connection without ever answering the agent protocol.
			defer conn.Close()
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", agentSocket.Addr().String())

	server := startTestSSHServerWithAuth(t, &ssh.ServerConfig{
		// Reject the "none" method so the client has to ask the agent for keys.
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
	})
	ds := sshDataSource(server.addr, ssh.FingerprintSHA256(server.hostKey))

	start := time.Now()
	client, err := getSSHClient(context.Background(), ds, 200*time.Millisecond)
	require.Error(t, err)
	require.Nil(t, client)
	require.Less(t, time.Since(start), 5*time.Second, "a stalled ssh-agent must not hang the handshake")
}

func TestDialThroughTunnelCarriesData(t *testing.T) {
	t.Parallel()

	server := startTestSSHServer(t)
	client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, ssh.FingerprintSHA256(server.hostKey)))
	require.NoError(t, err)
	defer client.Close()

	conn, err := DialThroughTunnel(context.Background(), client, "tcp", startEchoServer(t))
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte("ping"))
	require.NoError(t, err)
	echoed := make([]byte, 4)
	_, err = io.ReadFull(conn, echoed)
	require.NoError(t, err)
	require.Equal(t, "ping", string(echoed))
}

func TestDialThroughTunnelTimesOutWhenTheServerNeverAnswers(t *testing.T) {
	t.Parallel()

	server := startStallingSSHServer(t)
	client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, ssh.FingerprintSHA256(server.hostKey)))
	require.NoError(t, err)
	defer client.Close()

	start := time.Now()
	conn, err := dialThroughTunnel(context.Background(), client, "tcp", "127.0.0.1:1", 200*time.Millisecond)
	require.Error(t, err)
	require.Nil(t, conn)
	require.Less(t, time.Since(start), 5*time.Second, "opening a channel must not hang")
}

func TestDeadlineConnInterruptsABlockedRead(t *testing.T) {
	t.Parallel()

	conn := tunnelToEchoServer(t)
	deadlineConn := &DeadlineConn{Conn: conn}
	defer deadlineConn.Close()
	require.NoError(t, deadlineConn.SetDeadline(time.Now().Add(50*time.Millisecond)))

	start := time.Now()
	_, err := deadlineConn.Read(make([]byte, 1))
	require.Error(t, err, "the deadline must unblock the read")
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestDeadlineConnKeepsTheTunnelAfterTheDeadlineIsCleared(t *testing.T) {
	t.Parallel()

	conn := tunnelToEchoServer(t)
	deadlineConn := &DeadlineConn{Conn: conn}
	defer deadlineConn.Close()
	require.NoError(t, deadlineConn.SetDeadline(time.Now().Add(50*time.Millisecond)))
	require.NoError(t, deadlineConn.SetDeadline(time.Time{}))

	time.Sleep(200 * time.Millisecond)
	_, err := deadlineConn.Write([]byte("ping"))
	require.NoError(t, err)
	echoed := make([]byte, 4)
	_, err = io.ReadFull(deadlineConn, echoed)
	require.NoError(t, err)
	require.Equal(t, "ping", string(echoed))
}

func tunnelToEchoServer(t *testing.T) net.Conn {
	t.Helper()

	server := startTestSSHServer(t)
	client, err := GetSSHClient(context.Background(), sshDataSource(server.addr, ssh.FingerprintSHA256(server.hostKey)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	conn, err := DialThroughTunnel(context.Background(), client, "tcp", startEchoServer(t))
	require.NoError(t, err)
	return conn
}

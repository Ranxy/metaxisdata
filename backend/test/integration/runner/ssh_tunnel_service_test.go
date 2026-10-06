//go:build integration

package runner

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strconv"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// TestSSHTunnelHostKeyRealServerIntegration drives the SSH data source path end
// to end: the real server connects to the MySQL source through an SSH bastion,
// which is only allowed when the bastion presents the host key configured on the
// data source.
func TestSSHTunnelHostKeyRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedMySQLServiceEnvNoReset(t)
	ctx := t.Context()
	bastion := startSSHBastion(t)

	instance, err := env.CreateMySQLInstance(ctx, "ssh-tunnel")
	require.NoError(t, err)
	defer func() {
		_, _ = env.DeleteInstance(ctx, instance.GetName())
	}()

	throughBastion := func(hostKey string) *v1pb.DataSource {
		return &v1pb.DataSource{
			Type:       v1pb.DataSourceType_READ_ONLY,
			Username:   "root",
			Password:   "root",
			Host:       env.MySQLHost,
			Port:       env.MySQLPort,
			SshHost:    bastion.host,
			SshPort:    bastion.port,
			SshUser:    "tunnel",
			SshHostKey: hostKey,
		}
	}

	// The configured fingerprint is the only accepted host key, and it survives
	// the store <-> API round trip.
	configured := ssh.FingerprintSHA256(bastion.hostKey)
	created, err := env.CreateDataSource(ctx, instance.GetName(), throughBastion(configured), "through-bastion", true)
	require.NoError(t, err, "the server must reach MySQL through the tunnel")
	require.Equal(t, configured, created.GetSshHostKey())

	// An unknown host key is refused during the handshake, before any database
	// credential crosses the tunnel.
	_, err = env.CreateDataSource(ctx, instance.GetName(), throughBastion(untrustedFingerprint(t)), "wrong-key", true)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.ErrorContains(t, err, "host key mismatch")

	// A data source with no trusted key cannot tunnel at all.
	_, err = env.CreateDataSource(ctx, instance.GetName(), throughBastion(""), "no-key", true)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.ErrorContains(t, err, "ssh_host_key")
}

type sshBastion struct {
	host    string
	port    string
	hostKey ssh.PublicKey
}

// startSSHBastion runs an SSH server in the test process that forwards
// direct-tcpip channels, which is what the server under test dials for a data
// source with ssh_host set.
func startSSHBastion(t *testing.T) sshBastion {
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

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSSHConn(conn, config)
		}
	}()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	return sshBastion{host: host, port: port, hostKey: signer.PublicKey()}
}

func untrustedFingerprint(t *testing.T) string {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)
	return ssh.FingerprintSHA256(signer.PublicKey())
}

func serveSSHConn(conn net.Conn, config *ssh.ServerConfig) {
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		go forwardSSHChannel(newChannel)
	}
}

func forwardSSHChannel(newChannel ssh.NewChannel) {
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

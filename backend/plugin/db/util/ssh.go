//nolint:revive
package util

import (
	"context"
	"crypto/subtle"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// SSHTimeout bounds both the SSH transport handshake (TCP connect, version
// exchange and key agreement) and opening a TCP channel through an established
// tunnel. A bastion that accepts the TCP connection but never completes the
// handshake, or never answers a channel request, must not be able to hold a
// caller — and one of the instance's connection slots — forever.
const SSHTimeout = 30 * time.Second

// GetSSHClient returns an SSH client for ds.
//
// The server's host key is checked against the data source's ssh_host_key; a
// data source that lists no key is refused, because the tunnel carries the
// database credentials in clear text and any host that answers on ssh_host
// could otherwise collect them.
func GetSSHClient(ctx context.Context, ds *storepb.DataSource) (*ssh.Client, error) {
	return getSSHClient(ctx, ds, SSHTimeout)
}

func getSSHClient(ctx context.Context, ds *storepb.DataSource, timeout time.Duration) (*ssh.Client, error) {
	if ds.GetSshHost() == "" {
		return nil, errors.New("ssh host must be set")
	}
	hostKeyCallback, err := sshHostKeyCallback(ds)
	if err != nil {
		return nil, err
	}
	// The handshake deadline is set on the connection below; ClientConfig.Timeout
	// is deliberately left unset because only ssh.Dial reads it, and it would not
	// cover the version exchange and key agreement anyway.
	sshConfig := &ssh.ClientConfig{
		User:            ds.GetSshUser(),
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: hostKeyCallback,
	}
	// The deadline that bounds the whole handshake: the client's own timeout, or
	// the caller's deadline when that is earlier.
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if ds.GetSshPrivateKey() != "" {
		signer, err := ssh.ParsePrivateKey([]byte(ds.GetSshPrivateKey()))
		if err != nil {
			return nil, err
		}
		sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(signer))
	} else {
		// Users may use ssh-agent to store the private key with passphrase,
		// we will try to connect to the ssh-agent to get the private key.
		if conn, err := net.DialTimeout("unix", os.Getenv("SSH_AUTH_SOCK"), timeout); err == nil {
			defer conn.Close()
			// The agent socket is read during authentication, which the SSH
			// connection's deadline does not cover, so a wedged agent gets its
			// own: otherwise it would hang the handshake past every bound above.
			_ = conn.SetDeadline(deadline)
			// Create a new instance of the ssh agent
			agentClient := agent.NewClient(conn)
			sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeysCallback(agentClient.Signers))
		}
	}
	// When there's a non empty password add the password AuthMethod.
	if ds.GetSshPassword() != "" {
		sshConfig.Auth = append(sshConfig.Auth, ssh.PasswordCallback(func() (string, error) {
			return ds.GetSshPassword(), nil
		}))
	}

	addr := net.JoinHostPort(ds.GetSshHost(), ds.GetSshPort())
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}
	// A caller that gives up must interrupt a handshake in progress, not wait
	// out the timeout above.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(clientConn, chans, reqs), nil
}

// sshHostKeyCallback verifies the SSH server's host key against the data
// source's ssh_host_key. Each configured line is a fingerprint (SHA256:... or
// MD5:...) or a public key line in known_hosts or authorized_keys form, which is
// what ssh-keyscan prints. The host column of a known_hosts line is deliberately
// ignored: the list is the administrator's assertion about this data source's
// ssh_host, so a line copied from a known_hosts file trusts that key here
// whatever host it was recorded for. Paste only the line for the bastion named
// in ssh_host.
func sshHostKeyCallback(ds *storepb.DataSource) (ssh.HostKeyCallback, error) {
	var fingerprints []string
	var keys []ssh.PublicKey
	for _, entry := range strings.Split(ds.GetSshHostKey(), "\n") {
		entry = strings.TrimSpace(entry)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if fingerprint := sshFingerprint(entry); fingerprint != "" {
			fingerprints = append(fingerprints, fingerprint)
			continue
		}
		key, err := parseSSHHostKey(entry)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid ssh_host_key entry %q", entry)
		}
		keys = append(keys, key)
	}
	if len(fingerprints) == 0 && len(keys) == 0 {
		return nil, errors.Errorf("ssh_host_key must list the trusted host key of %s (a SHA256:... fingerprint or the public key printed by ssh-keyscan); the connection is refused without it", ds.GetSshHost())
	}

	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		for _, trusted := range keys {
			if key.Type() == trusted.Type() && subtle.ConstantTimeCompare(key.Marshal(), trusted.Marshal()) == 1 {
				return nil
			}
		}
		presented := ssh.FingerprintSHA256(key)
		// FingerprintLegacyMD5 returns the bare colon-separated digest.
		legacy := "MD5:" + ssh.FingerprintLegacyMD5(key)
		for _, trusted := range fingerprints {
			if subtle.ConstantTimeCompare([]byte(trusted), []byte(presented)) == 1 ||
				subtle.ConstantTimeCompare([]byte(trusted), []byte(legacy)) == 1 {
				return nil
			}
		}
		return errors.Errorf("ssh host key mismatch for %s: presented %s is not in ssh_host_key", hostname, presented)
	}, nil
}

// sshFingerprint normalizes a fingerprint entry and returns "" when the entry is
// not a usable one. ssh-keygen prints the fingerprint followed by the key's
// comment, and MD5 fingerprints are not case normalized, so only the fingerprint
// token is kept and its case and padding are normalized. An entry with an empty
// digest is rejected as malformed rather than kept as a fingerprint that can
// never match.
func sshFingerprint(entry string) string {
	token := entry
	if index := strings.IndexAny(token, " \t"); index >= 0 {
		token = token[:index]
	}
	switch {
	case strings.HasPrefix(strings.ToUpper(token), "SHA256:"):
		digest := strings.TrimRight(token[len("SHA256:"):], "=")
		if digest == "" {
			return ""
		}
		return "SHA256:" + digest
	case strings.HasPrefix(strings.ToUpper(token), "MD5:"):
		digest := strings.ToLower(token[len("MD5:"):])
		if digest == "" {
			return ""
		}
		return "MD5:" + digest
	default:
		return ""
	}
}

// parseSSHHostKey parses one host key entry as a known_hosts line or as an
// authorized_keys public key line.
func parseSSHHostKey(entry string) (ssh.PublicKey, error) {
	if marker, _, key, _, _, err := ssh.ParseKnownHosts([]byte(entry)); err == nil {
		if marker != "" {
			return nil, errors.Errorf("host key marker %q is not supported", marker)
		}
		return key, nil
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(entry))
	if err != nil {
		return nil, errors.New("not a fingerprint, a known_hosts line or a public key")
	}
	return key, nil
}

// DialThroughTunnel opens a connection to addr from the SSH server. The channel
// open is bounded by the shorter of the caller's deadline and SSHTimeout, so a
// host that keeps the SSH session but never answers a channel request cannot
// hold a caller — for instance for the whole 15 minutes of a sync — forever. The
// returned connection is not tied to the context.
func DialThroughTunnel(ctx context.Context, client *ssh.Client, network, addr string) (net.Conn, error) {
	return dialThroughTunnel(ctx, client, network, addr, SSHTimeout)
}

func dialThroughTunnel(ctx context.Context, client *ssh.Client, network, addr string, timeout time.Duration) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return client.DialContext(dialCtx, network, addr)
}

// DeadlineConn adapts a tunnel connection, which rejects net.Conn deadlines
// ("ssh: tcpChan: deadline not supported"), to drivers that interrupt a blocked
// query by setting one. pgx watches the statement context and sets the
// connection deadline when it is cancelled or times out; this connection closes
// the tunnel at that moment, which unblocks the pending read or write, where a
// no-op implementation let the stalled query run on.
type DeadlineConn struct {
	net.Conn
	mu    sync.Mutex
	timer *time.Timer
}

func (c *DeadlineConn) SetDeadline(deadline time.Time) error      { return c.setDeadline(deadline) }
func (c *DeadlineConn) SetReadDeadline(deadline time.Time) error  { return c.setDeadline(deadline) }
func (c *DeadlineConn) SetWriteDeadline(deadline time.Time) error { return c.setDeadline(deadline) }

// Close stops the deadline timer before closing the tunnel.
func (c *DeadlineConn) Close() error {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	return c.Conn.Close()
}

func (c *DeadlineConn) setDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	// A zero deadline clears the previous one.
	if deadline.IsZero() {
		return nil
	}
	if remaining := time.Until(deadline); remaining > 0 {
		c.timer = time.AfterFunc(remaining, func() { _ = c.Conn.Close() })
		return nil
	}
	// An already expired deadline interrupts the pending I/O now.
	return c.Conn.Close()
}

const sshPortSize = 100

// PortFIFO is the fifo for SSH client port queue.
var PortFIFO = make(chan int, sshPortSize)

func init() {
	for i := 0; i < sshPortSize; i++ {
		PortFIFO <- i + 6113
	}
}

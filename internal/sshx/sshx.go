// Package sshx runs commands on an Unraid server over SSH.
package sshx

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Credentials authenticate to the server.
type Credentials struct {
	Username   string
	Password   string
	PrivateKey []byte
}

// Target is a server to connect to.
type Target struct {
	Host string
	Port int32
	// HostKey pins the server's key ("<type> <base64>"). Empty accepts any
	// key; the one seen is returned by Dial so the caller can record it.
	HostKey string
}

// Client is a connected SSH client.
type Client struct {
	c *ssh.Client
	// HostKey is the server key presented on connect ("<type> <base64>").
	HostKey string
}

// ErrHostKeyMismatch is returned when the server's key isn't the pinned one.
var ErrHostKeyMismatch = errors.New("ssh host key changed")

// FormatKey renders a public key as "<type> <base64>".
func FormatKey(k ssh.PublicKey) string {
	return k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal())
}

// Dial connects and authenticates.
func Dial(ctx context.Context, t Target, cr Credentials) (*Client, error) {
	var auth []ssh.AuthMethod
	if len(cr.PrivateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(cr.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("private key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cr.Password != "" {
		pw := cr.Password
		auth = append(auth, ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			ans := make([]string, len(qs))
			for i := range qs {
				ans[i] = pw
			}
			return ans, nil
		}))
	}
	if len(auth) == 0 {
		return nil, errors.New("credentials hold neither a password nor a private key")
	}
	seen := ""
	cfg := &ssh.ClientConfig{
		User: cr.Username,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = FormatKey(key)
			if t.HostKey != "" && strings.TrimSpace(t.HostKey) != seen {
				return fmt.Errorf("%w: expected %q, got %q", ErrHostKeyMismatch, t.HostKey, seen)
			}
			return nil
		},
		Timeout: 15 * time.Second,
	}
	port := t.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(int(port)))
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &Client{c: ssh.NewClient(sc, chans, reqs), HostKey: seen}, nil
}

// Close ends the connection.
func (c *Client) Close() error { return c.c.Close() }

// Result is a command's outcome.
type Result struct {
	Stdout, Stderr string
	ExitCode       int
}

// Run runs a shell command, with optional stdin. A non-zero exit is not an
// error; check ExitCode.
func (c *Client) Run(ctx context.Context, cmd string, stdin io.Reader) (*Result, error) {
	s, err := c.c.NewSession()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var out, errb bytes.Buffer
	s.Stdout, s.Stderr, s.Stdin = &out, &errb, stdin
	done := make(chan error, 1)
	go func() { done <- s.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = s.Signal(ssh.SIGKILL)
		return nil, ctx.Err()
	case err := <-done:
		r := &Result{Stdout: out.String(), Stderr: errb.String()}
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			r.ExitCode = ee.ExitStatus()
			return r, nil
		}
		return r, err
	}
}

// MustRun runs a command and fails on a non-zero exit.
func (c *Client) MustRun(ctx context.Context, cmd string, stdin io.Reader) (string, error) {
	r, err := c.Run(ctx, cmd, stdin)
	if err != nil {
		return "", err
	}
	if r.ExitCode != 0 {
		msg := strings.TrimSpace(r.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(r.Stdout)
		}
		return r.Stdout, fmt.Errorf("exit %d: %s", r.ExitCode, msg)
	}
	return r.Stdout, nil
}

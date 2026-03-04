package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"warehouse-ui/internal/logger"
)

// SSHConfig holds the parameters for an SSH tunnel.
type SSHConfig struct {
	Host       string // SSH server host:port (final hop)
	User       string
	Password   string // password auth (optional)
	KeyPath    string // path to private key file (optional)
	RemoteHost string // database host as seen from SSH server
	RemotePort string // database port as seen from SSH server
	JumpHost   string // optional ProxyJump host (user@host or user@host:port)
}

// Tunnel represents an active SSH tunnel.
type Tunnel struct {
	localAddr  string
	listener   net.Listener
	client     *ssh.Client
	jumpClient *ssh.Client // non-nil when using ProxyJump
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// LocalAddr returns the local address (host:port) to connect to.
func (t *Tunnel) LocalAddr() string {
	return t.localAddr
}

// Close shuts down the tunnel.
func (t *Tunnel) Close() error {
	if t.cancel != nil {
		t.cancel()
	}
	var firstErr error
	if t.listener != nil {
		if err := t.listener.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	t.wg.Wait()
	if t.client != nil {
		if err := t.client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if t.jumpClient != nil {
		if err := t.jumpClient.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Open establishes an SSH tunnel and returns a Tunnel that forwards
// a local port to remoteHost:remotePort through the SSH server.
// If cfg.JumpHost is set, it first connects to the jump host (ProxyJump).
// Host aliases from ~/.ssh/config are resolved automatically.
func Open(ctx context.Context, cfg SSHConfig) (*Tunnel, error) {
	// Resolve SSH config aliases for both host and jump host
	sshConfigs := loadSSHConfig()
	cfg.Host = resolveSSHHost(cfg.Host, cfg.User, sshConfigs)
	if cfg.JumpHost == "" {
		// Check if ~/.ssh/config defines a ProxyJump for this host
		if entry, ok := sshConfigs[cfg.Host]; ok && entry.proxyJump != "" {
			cfg.JumpHost = entry.proxyJump
			logger.Info("ssh tunnel: auto-detected ProxyJump %s from ~/.ssh/config", cfg.JumpHost)
		}
		// Also check the original host name before port was added
		hostWithoutPort := cfg.Host
		if h, _, err := net.SplitHostPort(cfg.Host); err == nil {
			hostWithoutPort = h
		}
		for alias, entry := range sshConfigs {
			if entry.hostname == hostWithoutPort && entry.proxyJump != "" && cfg.JumpHost == "" {
				cfg.JumpHost = entry.proxyJump
				logger.Info("ssh tunnel: auto-detected ProxyJump %s for %s from ~/.ssh/config", cfg.JumpHost, alias)
			}
		}
	}
	if cfg.JumpHost != "" {
		cfg.JumpHost = resolveSSHHost(cfg.JumpHost, cfg.User, sshConfigs)
	}

	sshHost := cfg.Host
	if !strings.Contains(sshHost, ":") {
		sshHost += ":22"
	}

	authMethods := buildAuthMethods(cfg)
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("ssh: no authentication method provided (need password or key file)")
	}

	sshConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	var jumpClient *ssh.Client
	var client *ssh.Client

	if cfg.JumpHost != "" {
		// ProxyJump: connect to jump host first, then dial final host through it
		jumpUser, jumpHost := parseJumpHost(cfg.JumpHost, cfg.User)
		if !strings.Contains(jumpHost, ":") {
			jumpHost += ":22"
		}

		// Jump host uses the same auth methods (keys from ~/.ssh)
		jumpConfig := &ssh.ClientConfig{
			User:            jumpUser,
			Auth:            authMethods,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         10 * time.Second,
		}

		logger.Info("ssh tunnel: connecting to jump host %s@%s", jumpUser, jumpHost)

		var err error
		jumpClient, err = ssh.Dial("tcp", jumpHost, jumpConfig)
		if err != nil {
			return nil, fmt.Errorf("ssh jump host dial %s: %w", jumpHost, err)
		}

		// Dial final host through the jump connection
		logger.Info("ssh tunnel: jumping to %s@%s", cfg.User, sshHost)

		netConn, err := jumpClient.Dial("tcp", sshHost)
		if err != nil {
			jumpClient.Close()
			return nil, fmt.Errorf("ssh dial %s via jump: %w", sshHost, err)
		}

		ncc, chans, reqs, err := ssh.NewClientConn(netConn, sshHost, sshConfig)
		if err != nil {
			netConn.Close()
			jumpClient.Close()
			return nil, fmt.Errorf("ssh handshake %s via jump: %w", sshHost, err)
		}
		client = ssh.NewClient(ncc, chans, reqs)
	} else {
		logger.Info("ssh tunnel: connecting to %s@%s", cfg.User, sshHost)

		var err error
		client, err = ssh.Dial("tcp", sshHost, sshConfig)
		if err != nil {
			return nil, fmt.Errorf("ssh dial %s: %w", sshHost, err)
		}
	}

	// Listen on a random local port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
		return nil, fmt.Errorf("ssh tunnel: listen local: %w", err)
	}

	remoteAddr := net.JoinHostPort(cfg.RemoteHost, cfg.RemotePort)
	localAddr := listener.Addr().String()

	logger.Info("ssh tunnel: %s -> %s@%s -> %s", localAddr, cfg.User, sshHost, remoteAddr)

	tunnelCtx, cancel := context.WithCancel(ctx)

	t := &Tunnel{
		localAddr:  localAddr,
		listener:   listener,
		client:     client,
		jumpClient: jumpClient,
		cancel:     cancel,
	}

	// Accept connections in background
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			localConn, err := listener.Accept()
			if err != nil {
				select {
				case <-tunnelCtx.Done():
					return
				default:
					logger.Error("ssh tunnel: accept error: %v", err)
					return
				}
			}

			t.wg.Add(1)
			go func() {
				defer t.wg.Done()
				t.forward(tunnelCtx, localConn, remoteAddr)
			}()
		}
	}()

	return t, nil
}

// parseJumpHost extracts user and host from "user@host:port" or just "host".
func parseJumpHost(jump, defaultUser string) (string, string) {
	user := defaultUser
	host := jump
	if i := strings.Index(jump, "@"); i >= 0 {
		user = jump[:i]
		host = jump[i+1:]
	}
	return user, host
}

func (t *Tunnel) forward(ctx context.Context, localConn net.Conn, remoteAddr string) {
	defer localConn.Close()

	remoteConn, err := t.client.Dial("tcp", remoteAddr)
	if err != nil {
		logger.Error("ssh tunnel: remote dial %s: %v", remoteAddr, err)
		return
	}
	defer remoteConn.Close()

	done := make(chan struct{}, 2)

	go func() {
		io.Copy(remoteConn, localConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(localConn, remoteConn)
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}

func buildAuthMethods(cfg SSHConfig) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	// Key-based auth
	if cfg.KeyPath != "" {
		if key, err := loadPrivateKey(cfg.KeyPath, cfg.Password); err == nil {
			methods = append(methods, ssh.PublicKeys(key))
		} else {
			logger.Error("ssh: failed to load key %s: %v", cfg.KeyPath, err)
		}
	}

	// Try default key paths if no explicit key was given
	if cfg.KeyPath == "" {
		for _, name := range []string{"id_rsa", "id_ed25519", "id_ecdsa"} {
			home, _ := os.UserHomeDir()
			path := home + "/.ssh/" + name
			if _, err := os.Stat(path); err == nil {
				if key, err := loadPrivateKey(path, ""); err == nil {
					methods = append(methods, ssh.PublicKeys(key))
					break
				}
			}
		}
	}

	// Password auth
	if cfg.Password != "" && cfg.KeyPath == "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}

	return methods
}

func loadPrivateKey(path, passphrase string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	}
	return ssh.ParsePrivateKey(data)
}

// sshConfigEntry holds parsed fields from ~/.ssh/config for a single Host block.
type sshConfigEntry struct {
	hostname  string // Hostname directive
	user      string // User directive
	port      string // Port directive
	proxyJump string // ProxyJump directive
}

// loadSSHConfig parses ~/.ssh/config and returns a map of Host alias -> entry.
func loadSSHConfig() map[string]*sshConfigEntry {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return nil
	}
	defer f.Close()

	entries := make(map[string]*sshConfigEntry)
	var currentHosts []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value := parseSSHConfigLine(line)
		if key == "" {
			continue
		}

		switch strings.ToLower(key) {
		case "host":
			// New host block — may have multiple space-separated patterns
			currentHosts = strings.Fields(value)
			for _, h := range currentHosts {
				if strings.Contains(h, "*") || strings.Contains(h, "?") {
					continue // skip wildcard patterns
				}
				if _, ok := entries[h]; !ok {
					entries[h] = &sshConfigEntry{}
				}
			}
		case "hostname":
			for _, h := range currentHosts {
				if e, ok := entries[h]; ok {
					e.hostname = value
				}
			}
		case "user":
			for _, h := range currentHosts {
				if e, ok := entries[h]; ok {
					e.user = value
				}
			}
		case "port":
			for _, h := range currentHosts {
				if e, ok := entries[h]; ok {
					e.port = value
				}
			}
		case "proxyjump":
			for _, h := range currentHosts {
				if e, ok := entries[h]; ok {
					e.proxyJump = value
				}
			}
		}
	}
	return entries
}

// parseSSHConfigLine splits "Key Value" or "Key=Value".
func parseSSHConfigLine(line string) (string, string) {
	if i := strings.Index(line, "="); i >= 0 {
		return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
	}
	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		parts = strings.SplitN(line, "\t", 2)
	}
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// resolveSSHHost resolves an SSH config alias to its real hostname.
// Handles "user@host" format and preserves the user if present.
func resolveSSHHost(host, defaultUser string, configs map[string]*sshConfigEntry) string {
	if configs == nil {
		return host
	}

	// Extract user@ prefix if present
	user := ""
	bare := host
	if i := strings.Index(host, "@"); i >= 0 {
		user = host[:i+1] // includes @
		bare = host[i+1:]
	}

	// Strip port if present for lookup
	lookupHost := bare
	port := ""
	if h, p, err := net.SplitHostPort(bare); err == nil {
		lookupHost = h
		port = p
	}

	if entry, ok := configs[lookupHost]; ok && entry.hostname != "" {
		resolved := entry.hostname
		if port != "" {
			resolved = net.JoinHostPort(resolved, port)
		} else if entry.port != "" {
			resolved = net.JoinHostPort(resolved, entry.port)
		}
		if user == "" && entry.user != "" {
			user = entry.user + "@"
		}
		logger.Info("ssh config: resolved %s -> %s%s", lookupHost, user, resolved)
		return user + resolved
	}

	return host
}

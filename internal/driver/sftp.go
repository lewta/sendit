package driver

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/policy"
	"github.com/lewta/sendit/internal/task"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	defaultSFTPTimeoutS    = 30
	defaultSFTPPort        = 22
	defaultSFTPFileSize    = 1024
	eicarTestFile          = "X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"
	sftpOperationUpload    = "upload"
	sftpOperationDownload  = "download"
	sftpOperationList      = "list"
	sftpAuthMethodPassword = "password"
	sftpAuthMethodKey      = "publickey"
)

// SFTPErrorToHTTP is exported for tests.
var SFTPErrorToHTTP = sftpErrorToHTTP

var errSFTPConnectionClosed = errors.New("sftp connection is closed")

type sftpConnection struct {
	raw          net.Conn
	ssh          *ssh.Client
	client       *sftp.Client
	authMaterial string
	operation    chan struct{}
	closed       chan struct{}
	closeOnce    sync.Once
}

type sftpConnectionAttempt struct {
	done chan struct{}
}

// SFTPDriver executes upload, download, and list operations over SFTP.
type SFTPDriver struct {
	mu         sync.Mutex
	conns      map[string]*sftpConnection
	connecting map[string]*sftpConnectionAttempt
}

// NewSFTPDriver creates an SFTP driver with a shared connection cache.
func NewSFTPDriver() *SFTPDriver {
	return &SFTPDriver{
		conns:      make(map[string]*sftpConnection),
		connecting: make(map[string]*sftpConnectionAttempt),
	}
}

// Execute performs the SFTP operation described by t.
func (d *SFTPDriver) Execute(ctx context.Context, t task.Task) task.Result {
	start := time.Now()
	meta := make(map[string]string)

	u, err := url.Parse(t.URL)
	if err != nil {
		return task.Result{Task: t, Duration: time.Since(start), Error: fmt.Errorf("parsing URL: %w", err), Meta: meta}
	}
	if u.Hostname() == "" {
		return task.Result{Task: t, Duration: time.Since(start), Error: fmt.Errorf("sftp URL must include a host"), Meta: meta}
	}

	cfg := t.Config.SFTP
	timeoutS := cfg.TimeoutS
	if timeoutS <= 0 {
		timeoutS = defaultSFTPTimeoutS
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutS)*time.Second)
	defer cancel()

	addr := sftpAddress(u, cfg.Port)
	cacheKey := sftpCacheKey(addr, cfg)
	conn, err := d.getConn(callCtx, addr, cacheKey, cfg, meta)
	if err != nil {
		markSFTPErrorPhase(meta, "connect", err)
		return task.Result{
			Task:       t,
			StatusCode: sftpErrorToHTTP(err),
			Duration:   time.Since(start),
			Meta:       meta,
		}
	}

	operation := cfg.Operation
	if operation == "" {
		operation = sftpOperationUpload
	}
	path := u.Path
	if path == "" {
		path = "."
	}

	perform := func(operationConn *sftpConnection) (int64, error) {
		switch operation {
		case sftpOperationUpload:
			return sftpUpload(callCtx, operationConn.client, path, sftpPayload(cfg))
		case sftpOperationDownload:
			return sftpDownload(callCtx, operationConn.client, path)
		case sftpOperationList:
			return sftpList(callCtx, operationConn.client, path, meta)
		default:
			return 0, fmt.Errorf("unsupported sftp operation %q", operation)
		}
	}
	bytesRead, started, errorPhase, operationConn, opErr := d.executeSFTPOperation(callCtx, addr, cacheKey, cfg, meta, conn, perform)
	if opErr != nil {
		markSFTPErrorPhase(meta, errorPhase, opErr)
		if started {
			d.evict(cacheKey, operationConn)
		}
		return task.Result{
			Task:       t,
			StatusCode: sftpErrorToHTTP(opErr),
			Duration:   time.Since(start),
			BytesRead:  bytesRead,
			Meta:       meta,
		}
	}

	return task.Result{
		Task:       t,
		StatusCode: 200,
		Duration:   time.Since(start),
		BytesRead:  bytesRead,
		Meta:       meta,
	}
}

func (d *SFTPDriver) executeSFTPOperation(
	ctx context.Context,
	addr, cacheKey string,
	cfg config.SFTPConfig,
	meta map[string]string,
	conn *sftpConnection,
	perform func(*sftpConnection) (int64, error),
) (bytesRead int64, started bool, errorPhase string, operationConn *sftpConnection, opErr error) {
	errorPhase = "operation"
	operationConn = conn
	for attempt := 0; attempt < 2; attempt++ {
		bytesRead, started, opErr = operationConn.execute(ctx, func() (int64, error) {
			return perform(operationConn)
		})
		if !errors.Is(opErr, errSFTPConnectionClosed) || ctx.Err() != nil || started || attempt == 1 {
			break
		}
		var err error
		errorPhase = "connect"
		operationConn, err = d.getConn(ctx, addr, cacheKey, cfg, meta)
		if err != nil {
			opErr = err
			break
		}
		errorPhase = "operation"
	}
	return bytesRead, started, errorPhase, operationConn, opErr
}

func (d *SFTPDriver) getConn(ctx context.Context, addr, cacheKey string, cfg config.SFTPConfig, meta map[string]string) (*sftpConnection, error) {
	authMaterial := sftpAuthMaterial(cfg)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var stale *sftpConnection
		d.mu.Lock()
		if conn, ok := d.conns[cacheKey]; ok {
			if conn.authMaterial == authMaterial && !conn.isClosed() {
				populateCachedSFTPMetadata(conn, cfg, meta)
				d.mu.Unlock()
				return conn, nil
			}
			stale = conn
			delete(d.conns, cacheKey)
		}
		if attempt := d.connecting[cacheKey]; attempt != nil {
			d.mu.Unlock()
			if stale != nil {
				stale.close()
			}
			select {
			case <-attempt.done:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		attempt := &sftpConnectionAttempt{done: make(chan struct{})}
		d.connecting[cacheKey] = attempt
		d.mu.Unlock()
		if stale != nil {
			stale.close()
		}

		conn, err := dialSFTPConnection(ctx, addr, cfg, authMaterial, meta)
		d.mu.Lock()
		delete(d.connecting, cacheKey)
		if err == nil {
			d.conns[cacheKey] = conn
		}
		close(attempt.done)
		d.mu.Unlock()
		return conn, err
	}
}

func dialSFTPConnection(ctx context.Context, addr string, cfg config.SFTPConfig, authMaterial string, meta map[string]string) (*sftpConnection, error) {
	sshCfg, err := sftpSSHConfig(ctx, cfg, meta)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	stopCancellation := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stopCancellation()

	sshConn, chans, reqs, err := ssh.NewClientConn(rawConn, addr, sshCfg)
	if err != nil {
		_ = rawConn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	sshClient := ssh.NewClient(sshConn, chans, reqs)
	meta["sftp_server_version"] = strings.TrimSpace(string(sshClient.ServerVersion()))

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	if !stopCancellation() || ctx.Err() != nil {
		_ = sftpClient.Close()
		_ = sshClient.Close()
		return nil, ctx.Err()
	}

	conn := &sftpConnection{
		raw: rawConn, ssh: sshClient, client: sftpClient, authMaterial: authMaterial,
		operation: make(chan struct{}, 1), closed: make(chan struct{}),
	}
	conn.operation <- struct{}{}
	return conn, nil
}

func (d *SFTPDriver) evict(cacheKey string, failed *sftpConnection) {
	d.mu.Lock()
	if d.conns[cacheKey] == failed {
		delete(d.conns, cacheKey)
	}
	d.mu.Unlock()

	if failed != nil {
		failed.close()
	}
}

func (c *sftpConnection) execute(ctx context.Context, operation func() (int64, error)) (int64, bool, error) {
	if err := c.acquire(ctx); err != nil {
		return 0, false, err
	}
	defer c.release()
	fail := func(bytesRead int64, err error) (int64, bool, error) {
		// Mark the connection closed while still holding the operation token.
		// Waiters must observe a pre-start closure and reconnect rather than
		// starting on the failed session between release and cache eviction.
		c.close()
		return bytesRead, true, err
	}

	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		if err := c.raw.SetDeadline(deadline); err != nil {
			return fail(0, err)
		}
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = c.raw.SetDeadline(time.Now()) })
	bytesRead, err := operation()
	stopped := stopCancellation()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fail(bytesRead, ctxErr)
	}
	// The socket deadline and the context timer expire at the same instant.
	// A read can therefore surface a protocol-level connection error just
	// before the context goroutine records DeadlineExceeded. Attribute that
	// boundary to the configured operation timeout consistently.
	if hasDeadline && !time.Now().Before(deadline) {
		return fail(bytesRead, context.DeadlineExceeded)
	}
	if !stopped {
		return fail(bytesRead, errSFTPConnectionClosed)
	}
	if err != nil {
		return fail(bytesRead, err)
	}
	if err := c.raw.SetDeadline(time.Time{}); err != nil {
		return fail(bytesRead, err)
	}
	return bytesRead, true, nil
}

func (c *sftpConnection) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errSFTPConnectionClosed
	case <-c.operation:
	}
	if err := ctx.Err(); err != nil {
		c.release()
		return err
	}
	select {
	case <-c.closed:
		c.release()
		return errSFTPConnectionClosed
	default:
		return nil
	}
}

func (c *sftpConnection) release() {
	select {
	case <-c.closed:
	case c.operation <- struct{}{}:
	}
}

func (c *sftpConnection) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.raw != nil {
			_ = c.raw.Close()
		}
		if c.client != nil {
			_ = c.client.Close()
		}
		if c.ssh != nil {
			_ = c.ssh.Close()
		}
	})
}

func (c *sftpConnection) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func markSFTPErrorPhase(meta map[string]string, phase string, err error) {
	meta["sftp_error_phase"] = phase
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		meta["sftp_timeout_phase"] = phase
		return
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		meta["sftp_timeout_phase"] = phase
	}
}

func sftpSSHConfig(ctx context.Context, cfg config.SFTPConfig, meta map[string]string) (*ssh.ClientConfig, error) {
	methods, methodName, err := sftpAuthMethods(ctx, cfg)
	if err != nil {
		return nil, err
	}
	meta["sftp_auth_methods"] = methodName

	hostKeyCallback, err := sftpHostKeyCallback(ctx, cfg, meta)
	if err != nil {
		return nil, err
	}

	return &ssh.ClientConfig{
		User:              cfg.Username,
		Auth:              methods,
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: cfg.AllowedHostKeyTypes,
		Config: ssh.Config{
			Ciphers:      cfg.AllowedCiphers,
			KeyExchanges: cfg.AllowedKEX,
			MACs:         cfg.AllowedMACs,
		},
		Timeout: time.Duration(sftpTimeoutS(cfg)) * time.Second,
	}, nil
}

func sftpHostKeyCallback(ctx context.Context, cfg config.SFTPConfig, meta map[string]string) (ssh.HostKeyCallback, error) {
	var verifier ssh.HostKeyCallback
	if !cfg.Insecure {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolving home directory for known_hosts: %w", err)
		}
		knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
		if err := policy.FromContext(ctx).CheckReadPath(knownHostsPath); err != nil {
			return nil, fmt.Errorf("loading known_hosts %q: %w", knownHostsPath, err)
		}
		verifier, err = knownhosts.New(knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("loading known_hosts %q: %w", knownHostsPath, err)
		}
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		meta["sftp_host_key_type"] = key.Type()
		meta["sftp_host_key_fp"] = ssh.FingerprintSHA256(key)
		if len(cfg.AllowedHostKeyTypes) > 0 && !stringInList(key.Type(), cfg.AllowedHostKeyTypes) {
			return fmt.Errorf("sftp host key type %q is not allowed", key.Type())
		}
		if verifier != nil {
			return verifier(hostname, remote, key)
		}
		return nil
	}, nil
}

func sftpAuthMethods(ctx context.Context, cfg config.SFTPConfig) ([]ssh.AuthMethod, string, error) {
	if cfg.Password != "" {
		return []ssh.AuthMethod{ssh.Password(cfg.Password)}, sftpAuthMethodPassword, nil
	}
	if cfg.PrivateKey == "" {
		return nil, "", errors.New("sftp requires password or private_key")
	}

	keyData, err := sftpPrivateKeyData(ctx, cfg.PrivateKey)
	if err != nil {
		return nil, "", err
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, "", fmt.Errorf("parsing sftp private_key: %w", err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, sftpAuthMethodKey, nil
}

func sftpPrivateKeyData(ctx context.Context, value string) ([]byte, error) {
	if strings.Contains(value, "BEGIN ") {
		return []byte(value), nil
	}
	data, err := policy.ReadFile(policy.FromContext(ctx), value, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("reading sftp private_key: %w", err)
	}
	return data, nil
}

func sftpUpload(ctx context.Context, client *sftp.Client, path string, payload []byte) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f, err := client.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	n, err := io.Copy(f, bytes.NewReader(payload))
	if err != nil {
		return n, err
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	return n, nil
}

func sftpDownload(ctx context.Context, client *sftp.Client, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f, err := client.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	n, err := io.Copy(io.Discard, f)
	if err != nil {
		return n, err
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	return n, nil
}

func sftpList(ctx context.Context, client *sftp.Client, path string, meta map[string]string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	entries, err := client.ReadDir(path)
	if err != nil {
		return 0, err
	}
	meta["sftp_entry_count"] = strconv.Itoa(len(entries))
	return int64(len(entries)), ctx.Err()
}

func sftpPayload(cfg config.SFTPConfig) []byte {
	if cfg.EICAR {
		return []byte(eicarTestFile)
	}

	size := cfg.FileSizeBytes
	if size <= 0 {
		minSize := cfg.FileSizeMinBytes
		maxSize := cfg.FileSizeMaxBytes
		if minSize > 0 || maxSize > 0 {
			if maxSize <= 0 {
				maxSize = minSize
			}
			if minSize <= 0 {
				minSize = maxSize
			}
			size = minSize
			if maxSize > minSize {
				size += secureInt64N(maxSize - minSize + 1)
			}
		}
	}
	if size <= 0 {
		size = defaultSFTPFileSize
	}

	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte('a' + (i % 26))
	}
	return payload
}

func secureInt64N(n int64) int64 {
	if n <= 0 {
		return 0
	}
	v, err := cryptorand.Int(cryptorand.Reader, big.NewInt(n))
	if err != nil {
		return 0
	}
	return v.Int64()
}

func sftpAddress(u *url.URL, cfgPort int) string {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		portNum := cfgPort
		if portNum <= 0 {
			portNum = defaultSFTPPort
		}
		port = strconv.Itoa(portNum)
	}
	return net.JoinHostPort(host, port)
}

func sftpTimeoutS(cfg config.SFTPConfig) int {
	if cfg.TimeoutS > 0 {
		return cfg.TimeoutS
	}
	return defaultSFTPTimeoutS
}

func populateCachedSFTPMetadata(conn *sftpConnection, cfg config.SFTPConfig, meta map[string]string) {
	if conn == nil || conn.ssh == nil {
		return
	}
	meta["sftp_server_version"] = strings.TrimSpace(string(conn.ssh.ServerVersion()))
	if cfg.Password != "" {
		meta["sftp_auth_methods"] = sftpAuthMethodPassword
	} else if cfg.PrivateKey != "" {
		meta["sftp_auth_methods"] = sftpAuthMethodKey
	}
}

func sftpCacheKey(addr string, cfg config.SFTPConfig) string {
	var b strings.Builder
	writeCachePart(&b, addr)
	writeCachePart(&b, cfg.Username)
	writeCachePart(&b, sftpAuthMethod(cfg))
	writeCachePart(&b, strconv.FormatBool(cfg.Insecure))
	writeCachePart(&b, strings.Join(cfg.AllowedCiphers, ","))
	writeCachePart(&b, strings.Join(cfg.AllowedKEX, ","))
	writeCachePart(&b, strings.Join(cfg.AllowedHostKeyTypes, ","))
	writeCachePart(&b, strings.Join(cfg.AllowedMACs, ","))
	return b.String()
}

func sftpAuthMethod(cfg config.SFTPConfig) string {
	if cfg.Password != "" {
		return sftpAuthMethodPassword
	}
	if cfg.PrivateKey != "" {
		return sftpAuthMethodKey
	}
	return ""
}

func sftpAuthMaterial(cfg config.SFTPConfig) string {
	if cfg.Password != "" {
		return sftpAuthMethodPassword + ":" + cfg.Password
	}
	if cfg.PrivateKey != "" {
		return sftpAuthMethodKey + ":" + cfg.PrivateKey
	}
	return ""
}

func writeCachePart(w io.Writer, s string) {
	_, _ = io.WriteString(w, strconv.Itoa(len(s)))
	_, _ = io.WriteString(w, ":")
	_, _ = io.WriteString(w, s)
	_, _ = io.WriteString(w, "|")
}

func sftpErrorToHTTP(err error) int {
	if err == nil {
		return 200
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return 504
	}
	if errors.Is(err, sftp.ErrSSHFxPermissionDenied) {
		return 403
	}
	if errors.Is(err, sftp.ErrSSHFxNoSuchFile) {
		return 404
	}
	if os.IsPermission(err) {
		return 403
	}
	if os.IsNotExist(err) {
		return 404
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "permission denied"):
		return 401
	case strings.Contains(msg, "access denied") || strings.Contains(msg, "failure") && strings.Contains(msg, "permission"):
		return 403
	case strings.Contains(msg, "no such file") || strings.Contains(msg, "file does not exist") || strings.Contains(msg, "not found"):
		return 404
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return 504
	case strings.Contains(msg, "host key") || strings.Contains(msg, "no common algorithm") || strings.Contains(msg, "handshake") || strings.Contains(msg, "ssh:"):
		return 502
	default:
		return 502
	}
}

func stringInList(needle string, haystack []string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

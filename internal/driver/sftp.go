package driver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lewta/sendit/internal/config"
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

type sftpConnection struct {
	ssh    *ssh.Client
	client *sftp.Client
}

// SFTPDriver executes upload, download, and list operations over SFTP.
type SFTPDriver struct {
	mu    sync.Mutex
	conns map[string]*sftpConnection
}

// NewSFTPDriver creates an SFTP driver with a shared connection cache.
func NewSFTPDriver() *SFTPDriver {
	return &SFTPDriver{conns: make(map[string]*sftpConnection)}
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

	var bytesRead int64
	var opErr error
	switch operation {
	case sftpOperationUpload:
		bytesRead, opErr = sftpUpload(callCtx, conn.client, path, sftpPayload(cfg))
	case sftpOperationDownload:
		bytesRead, opErr = sftpDownload(callCtx, conn.client, path)
	case sftpOperationList:
		bytesRead, opErr = sftpList(callCtx, conn.client, path, meta)
	default:
		opErr = fmt.Errorf("unsupported sftp operation %q", operation)
	}
	if opErr != nil {
		d.evict(cacheKey)
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

func (d *SFTPDriver) getConn(ctx context.Context, addr, cacheKey string, cfg config.SFTPConfig, meta map[string]string) (*sftpConnection, error) {
	d.mu.Lock()
	if conn, ok := d.conns[cacheKey]; ok {
		populateCachedSFTPMetadata(conn, cfg, meta)
		d.mu.Unlock()
		return conn, nil
	}
	d.mu.Unlock()

	sshCfg, err := sftpSSHConfig(cfg, meta)
	if err != nil {
		return nil, err
	}

	var dialer net.Dialer
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(rawConn, addr, sshCfg)
	if err != nil {
		_ = rawConn.Close()
		return nil, err
	}

	sshClient := ssh.NewClient(sshConn, chans, reqs)
	meta["sftp_server_version"] = strings.TrimSpace(string(sshClient.ServerVersion()))

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, err
	}

	conn := &sftpConnection{ssh: sshClient, client: sftpClient}
	d.mu.Lock()
	d.conns[cacheKey] = conn
	d.mu.Unlock()
	return conn, nil
}

func (d *SFTPDriver) evict(cacheKey string) {
	d.mu.Lock()
	conn := d.conns[cacheKey]
	delete(d.conns, cacheKey)
	d.mu.Unlock()

	if conn != nil {
		_ = conn.client.Close()
		_ = conn.ssh.Close()
	}
}

func sftpSSHConfig(cfg config.SFTPConfig, meta map[string]string) (*ssh.ClientConfig, error) {
	methods, methodName, err := sftpAuthMethods(cfg)
	if err != nil {
		return nil, err
	}
	meta["sftp_auth_methods"] = methodName

	hostKeyCallback, err := sftpHostKeyCallback(cfg, meta)
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

func sftpHostKeyCallback(cfg config.SFTPConfig, meta map[string]string) (ssh.HostKeyCallback, error) {
	var verifier ssh.HostKeyCallback
	if !cfg.Insecure {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolving home directory for known_hosts: %w", err)
		}
		knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
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

func sftpAuthMethods(cfg config.SFTPConfig) ([]ssh.AuthMethod, string, error) {
	if cfg.Password != "" {
		return []ssh.AuthMethod{ssh.Password(cfg.Password)}, sftpAuthMethodPassword, nil
	}
	if cfg.PrivateKey == "" {
		return nil, "", errors.New("sftp requires password or private_key")
	}

	keyData, err := sftpPrivateKeyData(cfg.PrivateKey)
	if err != nil {
		return nil, "", err
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, "", fmt.Errorf("parsing sftp private_key: %w", err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, sftpAuthMethodKey, nil
}

func sftpPrivateKeyData(value string) ([]byte, error) {
	if strings.Contains(value, "BEGIN ") {
		return []byte(value), nil
	}
	data, err := os.ReadFile(value)
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
				size += rand.Int64N(maxSize - minSize + 1)
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
	h := sha256.New()
	writeCachePart(h, addr)
	writeCachePart(h, cfg.Username)
	writeCachePart(h, cfg.Password)
	writeCachePart(h, cfg.PrivateKey)
	writeCachePart(h, strconv.FormatBool(cfg.Insecure))
	writeCachePart(h, strings.Join(cfg.AllowedCiphers, ","))
	writeCachePart(h, strings.Join(cfg.AllowedKEX, ","))
	writeCachePart(h, strings.Join(cfg.AllowedHostKeyTypes, ","))
	writeCachePart(h, strings.Join(cfg.AllowedMACs, ","))
	return hex.EncodeToString(h.Sum(nil))
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

//go:build darwin || linux

// Package sourceagentupdate contains the platform-neutral updater protocols
// and the macOS-first protected local storage implementation.
package sourceagentupdate

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	ConfigSchemaV1      = "source-agent-updater-config.v1"
	configMaximumBytes  = 4 << 10
	configURLMaxBytes   = 1 << 10
	configAgentMaxRunes = 128
)

var (
	ErrInvalidConfig       = errors.New("source agent updater config is invalid")
	ErrUnsafeConfigStorage = errors.New("source agent updater config storage is unsafe")
)

// Config is intentionally non-secret. The shared token is loaded through a
// separate credential mechanism and must never appear in this protocol.
type Config struct {
	Schema   string `json:"schema"`
	KBaseURL string `json:"kbase_url"`
	AgentID  string `json:"agent_id"`
}

// LoadConfig reads a strict, bounded config from a caller-selected fixed local
// path. The protected parent and file are opened without following symbolic
// links.
func LoadConfig(configPath string) (Config, error) {
	directoryFD, name, err := openProtectedConfigParent(configPath)
	if err != nil {
		return Config{}, err
	}
	defer unix.Close(directoryFD)

	fd, err := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return Config{}, ErrUnsafeConfigStorage
	}
	file := os.NewFile(uintptr(fd), "source-agent-updater-config")
	if file == nil {
		unix.Close(fd)
		return Config{}, ErrUnsafeConfigStorage
	}
	defer file.Close()
	if err := validateProtectedConfigFile(fd); err != nil {
		return Config{}, err
	}
	payload, err := io.ReadAll(io.LimitReader(file, configMaximumBytes+1))
	if err != nil || len(payload) == 0 || len(payload) > configMaximumBytes {
		return Config{}, ErrInvalidConfig
	}
	return decodeConfig(payload)
}

// SaveConfig validates before touching storage, then writes a private
// temporary file, fsyncs it, atomically renames it, and fsyncs the directory.
func SaveConfig(configPath string, config Config) error {
	if err := validateConfig(config); err != nil {
		return err
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return ErrInvalidConfig
	}
	payload = append(payload, '\n')
	if len(payload) > configMaximumBytes {
		return ErrInvalidConfig
	}

	directoryFD, name, err := openProtectedConfigParent(configPath)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	if err := validateExistingConfigEntry(directoryFD, name); err != nil {
		return err
	}

	temporary, fd, err := createProtectedConfigTemporary(directoryFD)
	if err != nil {
		return err
	}
	keepTemporary := true
	defer func() {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		if keepTemporary {
			_ = unix.Unlinkat(directoryFD, temporary, 0)
		}
	}()
	if err := writeAll(fd, payload); err != nil {
		return err
	}
	if err := unix.Fsync(fd); err != nil {
		return ErrUnsafeConfigStorage
	}
	if err := unix.Close(fd); err != nil {
		fd = -1
		return ErrUnsafeConfigStorage
	}
	fd = -1
	if err := unix.Renameat(directoryFD, temporary, directoryFD, name); err != nil {
		return ErrUnsafeConfigStorage
	}
	keepTemporary = false
	if err := unix.Fsync(directoryFD); err != nil {
		return ErrUnsafeConfigStorage
	}
	return nil
}

func decodeConfig(payload []byte) (Config, error) {
	if len(payload) == 0 || len(payload) > configMaximumBytes {
		return Config{}, ErrInvalidConfig
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return Config{}, ErrInvalidConfig
	}
	seen := make(map[string]struct{}, 3)
	config := Config{}
	for decoder.More() {
		rawKey, err := decoder.Token()
		key, ok := rawKey.(string)
		if err != nil || !ok {
			return Config{}, ErrInvalidConfig
		}
		if _, duplicate := seen[key]; duplicate {
			return Config{}, ErrInvalidConfig
		}
		seen[key] = struct{}{}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return Config{}, ErrInvalidConfig
		}
		switch key {
		case "schema":
			config.Schema = value
		case "kbase_url":
			config.KBaseURL = value
		case "agent_id":
			config.AgentID = value
		default:
			return Config{}, ErrInvalidConfig
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 3 {
		return Config{}, ErrInvalidConfig
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Config{}, ErrInvalidConfig
	}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateConfig(config Config) error {
	if config.Schema != ConfigSchemaV1 || !validNormalizedKBaseURL(config.KBaseURL) ||
		!validNormalizedAgentID(config.AgentID) {
		return ErrInvalidConfig
	}
	return nil
}

func validNormalizedKBaseURL(value string) bool {
	if value == "" || len(value) > configURLMaxBytes || strings.TrimSpace(value) != value ||
		strings.TrimRight(value, "/") != value {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		parsed.RawPath != "" || parsed.Scheme != strings.ToLower(parsed.Scheme) || parsed.Host != strings.ToLower(parsed.Host) {
		return false
	}
	if parsed.Path != "" && (pathpkg.Clean(parsed.Path) != parsed.Path || strings.Contains(parsed.Path, "//")) {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validNormalizedAgentID(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len([]rune(value)) > configAgentMaxRunes {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func openProtectedConfigParent(configPath string) (int, string, error) {
	if !filepath.IsAbs(configPath) || filepath.Clean(configPath) != configPath {
		return -1, "", ErrUnsafeConfigStorage
	}
	name := filepath.Base(configPath)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return -1, "", ErrUnsafeConfigStorage
	}
	parent := filepath.Dir(configPath)
	fd, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, "", ErrUnsafeConfigStorage
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		stat.Mode&0o7777 != 0o700 || stat.Uid != uint32(unix.Geteuid()) {
		unix.Close(fd)
		return -1, "", ErrUnsafeConfigStorage
	}
	return fd, name, nil
}

func validateExistingConfigEntry(directoryFD int, name string) error {
	var stat unix.Stat_t
	err := unix.Fstatat(directoryFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 ||
		stat.Uid != uint32(unix.Geteuid()) {
		return ErrUnsafeConfigStorage
	}
	return nil
}

func validateProtectedConfigFile(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0o7777 != 0o600 || stat.Uid != uint32(unix.Geteuid()) {
		return ErrUnsafeConfigStorage
	}
	if stat.Size <= 0 || stat.Size > configMaximumBytes {
		return ErrInvalidConfig
	}
	return nil
}

func createProtectedConfigTemporary(directoryFD int) (string, int, error) {
	var random [16]byte
	if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
		return "", -1, ErrUnsafeConfigStorage
	}
	name := fmt.Sprintf(".updater-config-%x", random[:])
	fd, err := unix.Openat(
		directoryFD, name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0o600,
	)
	if err != nil {
		return "", -1, ErrUnsafeConfigStorage
	}
	if err := validateProtectedConfigFileForWrite(fd); err != nil {
		unix.Close(fd)
		unix.Unlinkat(directoryFD, name, 0)
		return "", -1, err
	}
	return name, fd, nil
}

func validateProtectedConfigFileForWrite(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0o7777 != 0o600 || stat.Uid != uint32(unix.Geteuid()) || stat.Size != 0 {
		return ErrUnsafeConfigStorage
	}
	return nil
}

func writeAll(fd int, payload []byte) error {
	for len(payload) > 0 {
		written, err := unix.Write(fd, payload)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || written <= 0 {
			return ErrUnsafeConfigStorage
		}
		payload = payload[written:]
	}
	return nil
}

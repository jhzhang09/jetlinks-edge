// Package externalplugin 提供跨平台的进程外插件发现与生命周期适配。
//
// @author jhzhang
// @date 2026-09-22
package externalplugin

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

const ProtocolVersion = "jetlinks-edge-plugin/v1"

const (
	KindDriver = "driver"
	KindNorth  = "north"
)

// Manifest 描述一个可热插拔的外部进程插件。
type Manifest struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Command    string                   `json:"command"`
	Args       []string                 `json:"args,omitempty"`
	Descriptor core.ExtensionDescriptor `json:"descriptor"`

	manifestPath string
	commandPath  string
	fingerprint  string
}

func loadManifest(directory, path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", filepath.Base(path), err)
	}
	if manifest.APIVersion != ProtocolVersion {
		return Manifest{}, fmt.Errorf("manifest %s uses unsupported apiVersion %q", filepath.Base(path), manifest.APIVersion)
	}
	if manifest.Kind != KindDriver && manifest.Kind != KindNorth {
		return Manifest{}, fmt.Errorf("manifest %s has unsupported kind %q", filepath.Base(path), manifest.Kind)
	}
	if manifest.Descriptor.Type == "" || manifest.Descriptor.Name == "" || manifest.Descriptor.Version == "" {
		return Manifest{}, fmt.Errorf("manifest %s descriptor requires type, name and version", filepath.Base(path))
	}
	if manifest.Command == "" {
		return Manifest{}, fmt.Errorf("manifest %s command is required", filepath.Base(path))
	}
	commandPath := manifest.Command
	if !filepath.IsAbs(commandPath) {
		commandPath = filepath.Join(filepath.Dir(path), commandPath)
	}
	commandPath, err = filepath.Abs(commandPath)
	if err != nil {
		return Manifest{}, err
	}
	commandPath, err = filepath.EvalSymlinks(commandPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve plugin executable: %w", err)
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return Manifest{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve plugin directory: %w", err)
	}
	relative, err := filepath.Rel(root, commandPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Manifest{}, fmt.Errorf("manifest %s command must stay inside plugin directory", filepath.Base(path))
	}
	info, err := os.Stat(commandPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin executable %s: %w", commandPath, err)
	}
	if info.IsDir() || runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		return Manifest{}, fmt.Errorf("plugin command is not executable: %s", commandPath)
	}
	hasher := sha256.New()
	_, _ = hasher.Write(data)
	executable, err := os.Open(commandPath)
	if err != nil {
		return Manifest{}, err
	}
	if _, err := io.Copy(hasher, executable); err != nil {
		_ = executable.Close()
		return Manifest{}, err
	}
	if err := executable.Close(); err != nil {
		return Manifest{}, err
	}
	manifest.Descriptor.Runtime = "external-process"
	manifest.Descriptor.ProtocolVersion = ProtocolVersion
	manifest.manifestPath = path
	manifest.commandPath = commandPath
	manifest.fingerprint = fmt.Sprintf("%x", hasher.Sum(nil))
	return manifest, nil
}

func manifestKey(kind, pluginType string) string {
	return kind + ":" + pluginType
}

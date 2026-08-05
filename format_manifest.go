package renderer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// FormatManifestFilename is the fixed manifest name for a local Karte
	// Format package.
	FormatManifestFilename = "karte-format.yaml"
	// FormatSchemaVersion is the latest manifest schema understood by this
	// renderer.
	FormatSchemaVersion = 1
)

var (
	formatNameRe    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
	formatVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

// FormatManifest is the v0.1 contract stored in karte-format.yaml.
type FormatManifest struct {
	SchemaVersion int                    `yaml:"schemaVersion" json:"schemaVersion"`
	Name          string                 `yaml:"name" json:"name"`
	Version       string                 `yaml:"version" json:"version"`
	Markdown      FormatMarkdownManifest `yaml:"markdown" json:"markdown"`
	Marp          FormatMarpManifest     `yaml:"marp" json:"marp"`
	Assets        FormatAssetsManifest   `yaml:"assets" json:"assets"`
}

// FormatMarkdownManifest configures normal Markdown output.
type FormatMarkdownManifest struct {
	Layout string   `yaml:"layout" json:"layout"`
	Styles []string `yaml:"styles" json:"styles"`
}

// FormatMarpManifest configures output delegated to Marp.
type FormatMarpManifest struct {
	DefaultTheme string   `yaml:"defaultTheme" json:"defaultTheme"`
	Themes       []string `yaml:"themes" json:"themes"`
}

// FormatAssetsManifest names the shared asset directory.
type FormatAssetsManifest struct {
	Directory string `yaml:"directory" json:"directory"`
}

// LoadFormatManifest reads and validates karte-format.yaml in directory.
// Paths remain package-relative; resolving files and symlinks belongs to the
// package resolver.
func LoadFormatManifest(directory string) (FormatManifest, error) {
	manifestPath := filepath.Join(directory, FormatManifestFilename)
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return FormatManifest{}, fmt.Errorf("read format manifest %s: %w", manifestPath, err)
	}
	manifest, err := ParseFormatManifest(b)
	if err != nil {
		return FormatManifest{}, fmt.Errorf("parse format manifest %s: %w", manifestPath, err)
	}
	return manifest, nil
}

// ParseFormatManifest parses the v0.1 manifest contract. Unknown fields and
// trailing YAML documents are rejected so misspelled settings cannot silently
// fall back to renderer defaults.
func ParseFormatManifest(source []byte) (FormatManifest, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	decoder.KnownFields(true)
	var manifest FormatManifest
	if err := decoder.Decode(&manifest); err != nil {
		return FormatManifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return FormatManifest{}, errors.New("multiple YAML documents are not allowed")
		}
		return FormatManifest{}, err
	}
	if err := manifest.validate(); err != nil {
		return FormatManifest{}, err
	}
	return manifest, nil
}

func (m FormatManifest) validate() error {
	if m.SchemaVersion == 0 {
		return errors.New("schemaVersion is required")
	}
	if m.SchemaVersion != FormatSchemaVersion {
		return fmt.Errorf("unsupported schemaVersion %d (supported: %d)", m.SchemaVersion, FormatSchemaVersion)
	}
	if !formatNameRe.MatchString(m.Name) {
		return errors.New("name must use lowercase letters, digits, dots, underscores, or hyphens")
	}
	if !formatVersionRe.MatchString(m.Version) {
		return errors.New("version must be a semantic version such as 1.0.0")
	}
	if err := validateFormatPath("markdown.layout", m.Markdown.Layout); err != nil {
		return err
	}
	if len(m.Markdown.Styles) == 0 {
		return errors.New("markdown.styles must contain at least one stylesheet")
	}
	if err := validateFormatPaths("markdown.styles", m.Markdown.Styles); err != nil {
		return err
	}
	if strings.TrimSpace(m.Marp.DefaultTheme) == "" {
		return errors.New("marp.defaultTheme is required")
	}
	if len(m.Marp.Themes) == 0 {
		return errors.New("marp.themes must contain at least one theme")
	}
	if err := validateFormatPaths("marp.themes", m.Marp.Themes); err != nil {
		return err
	}
	if err := validateFormatPath("assets.directory", m.Assets.Directory); err != nil {
		return err
	}
	return nil
}

func validateFormatPaths(field string, values []string) error {
	seen := make(map[string]bool, len(values))
	for i, value := range values {
		if err := validateFormatPath(fmt.Sprintf("%s[%d]", field, i), value); err != nil {
			return err
		}
		cleaned := path.Clean(value)
		if seen[cleaned] {
			return fmt.Errorf("%s contains duplicate path %q", field, value)
		}
		seen[cleaned] = true
	}
	return nil
}

func validateFormatPath(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not have surrounding whitespace", field)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", field)
	}
	if strings.Contains(value, "\\") {
		return fmt.Errorf("%s must use forward slashes", field)
	}
	if path.IsAbs(value) || hasWindowsDrivePrefix(value) || strings.Contains(value, "://") {
		return fmt.Errorf("%s must be a package-relative local path", field)
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("%s escapes the format package", field)
	}
	return nil
}

func hasWindowsDrivePrefix(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':'
}

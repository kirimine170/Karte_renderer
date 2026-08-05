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

type formatManifestYAML struct {
	SchemaVersion formatManifestInt          `yaml:"schemaVersion"`
	Name          formatManifestString       `yaml:"name"`
	Version       formatManifestString       `yaml:"version"`
	Markdown      formatMarkdownManifestYAML `yaml:"markdown"`
	Marp          formatMarpManifestYAML     `yaml:"marp"`
	Assets        formatAssetsManifestYAML   `yaml:"assets"`
}

type formatMarkdownManifestYAML struct {
	Layout formatManifestString     `yaml:"layout"`
	Styles formatManifestStringList `yaml:"styles"`
}

type formatMarpManifestYAML struct {
	DefaultTheme formatManifestString     `yaml:"defaultTheme"`
	Themes       formatManifestStringList `yaml:"themes"`
}

type formatAssetsManifestYAML struct {
	Directory formatManifestString `yaml:"directory"`
}

type formatManifestInt int

func (value *formatManifestInt) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return fmt.Errorf("expected integer, got %s", yamlNodeType(node))
	}
	var decoded int
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*value = formatManifestInt(decoded)
	return nil
}

type formatManifestString string

func (value *formatManifestString) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return fmt.Errorf("expected string, got %s", yamlNodeType(node))
	}
	*value = formatManifestString(node.Value)
	return nil
}

type formatManifestStringList []string

func (values *formatManifestStringList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("expected string array, got %s", yamlNodeType(node))
	}
	decoded := make([]string, 0, len(node.Content))
	for i, child := range node.Content {
		if child.Kind != yaml.ScalarNode || child.Tag != "!!str" {
			return fmt.Errorf("item %d: expected string, got %s", i, yamlNodeType(child))
		}
		decoded = append(decoded, child.Value)
	}
	*values = decoded
	return nil
}

func yamlNodeType(node *yaml.Node) string {
	if node == nil {
		return "missing value"
	}
	if node.Tag != "" {
		return node.Tag
	}
	return fmt.Sprintf("YAML kind %d", node.Kind)
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
	var decoded formatManifestYAML
	if err := decoder.Decode(&decoded); err != nil {
		return FormatManifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return FormatManifest{}, errors.New("multiple YAML documents are not allowed")
		}
		return FormatManifest{}, err
	}
	manifest := FormatManifest{
		SchemaVersion: int(decoded.SchemaVersion),
		Name:          string(decoded.Name),
		Version:       string(decoded.Version),
		Markdown: FormatMarkdownManifest{
			Layout: string(decoded.Markdown.Layout),
			Styles: []string(decoded.Markdown.Styles),
		},
		Marp: FormatMarpManifest{
			DefaultTheme: string(decoded.Marp.DefaultTheme),
			Themes:       []string(decoded.Marp.Themes),
		},
		Assets: FormatAssetsManifest{Directory: string(decoded.Assets.Directory)},
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

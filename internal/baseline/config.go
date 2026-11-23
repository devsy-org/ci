package baseline

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

const configCheckCommand = "config-check"

var configKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func executeConfigCheck(args []string, stdout, stderr io.Writer) int {
	name, err := configCheckFlags(args)
	if err != nil {
		return runError(stderr, err)
	}
	tags, err := readConfigTags(name)
	if err != nil {
		return runError(stderr, err)
	}
	if _, err := fmt.Fprintln(stdout, strings.Join(tags, ",")); err != nil {
		return runError(stderr, fmt.Errorf("write build tags: %w", err))
	}
	return 0
}

func configCheckFlags(args []string) (string, error) {
	flags := flag.NewFlagSet(configCheckCommand, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("config", "", "trusted golangci-lint config")
	linters := flags.String("linters", "", "comma-separated baseline linters")
	if err := flags.Parse(args); err != nil {
		return "", fmt.Errorf("parse config-check flags: %w", err)
	}
	if *name == "" || flags.NArg() != 0 {
		return "", errors.New("expected --config and --linters with no positional arguments")
	}
	for linter := range strings.SplitSeq(*linters, ",") {
		switch linter {
		case "cyclop", "funlen", "gocognit", "gocyclo", "nestif", "revive":
		default:
			return "", errors.New(
				"expected comma-separated linter names in baseline-linters from the allowlist",
			)
		}
	}
	return *name, nil
}

func readConfigTags(name string) ([]string, error) {
	file, err := os.Open(filepath.Clean(name))
	if err != nil {
		return nil, fmt.Errorf("open lint config: %w", err)
	}
	tags, err := configTags(file)
	if err := errors.Join(err, file.Close()); err != nil {
		return nil, fmt.Errorf("check lint config: %w", err)
	}
	return tags, nil
}

func configTags(input io.Reader) ([]string, error) {
	doc, err := configDocument(input)
	if err != nil {
		return nil, err
	}
	if err := validateYAMLKeys(doc); err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := doc.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config mapping: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("expected a YAML config mapping")
	}
	folded, err := configMap(cfg, "")
	if err != nil {
		return nil, err
	}
	run, _ := folded["run"].(map[string]any)
	return buildTags(run["build-tags"])
}

func configDocument(input io.Reader) (*yaml.Node, error) {
	var doc, extra yaml.Node
	decoder := yaml.NewDecoder(input)
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode YAML document: %w", err)
	}
	err := decoder.Decode(&extra)
	if err == nil {
		return nil, errors.New("expected exactly one YAML document")
	}
	if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode trailing YAML: %w", err)
	}
	return &doc, nil
}

func validateYAMLKeys(node *yaml.Node) error {
	for i, child := range node.Content {
		if node.Kind == yaml.MappingNode && i%2 == 0 {
			if err := validateYAMLKey(child); err != nil {
				return err
			}
		}
		if err := validateYAMLKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func validateYAMLKey(key *yaml.Node) error {
	if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
		return fmt.Errorf("expected a string YAML mapping key at line %d", key.Line)
	}
	if !configKeyPattern.MatchString(key.Value) {
		return fmt.Errorf(
			"expected YAML mapping key to match %s at line %d",
			configKeyPattern,
			key.Line,
		)
	}
	return nil
}

func configMap(cfg map[string]any, prefix string) (map[string]any, error) {
	folded := make(map[string]any, len(cfg))
	for key, value := range cfg {
		key = strings.ToLower(key)
		if _, duplicate := folded[key]; duplicate {
			return nil, fmt.Errorf("duplicate case-insensitive config key %q", prefix+key)
		}
		if blockedConfigKey(prefix, key) {
			return nil, errors.New("base config uses revive.config, custom linters or plugins")
		}
		value, err := inspectConfig(value, prefix+key+".")
		if err != nil {
			return nil, err
		}
		folded[key] = value
	}
	return folded, nil
}

func blockedConfigKey(prefix, key string) bool {
	if key == "plugins" {
		return true
	}
	path := prefix + key + "."
	return strings.HasPrefix(path, "linters.settings.custom.") ||
		strings.HasPrefix(path, "linters.settings.revive.config.")
}

func inspectConfig(value any, prefix string) (any, error) {
	switch value := value.(type) {
	case map[string]any:
		return configMap(value, prefix)
	case []any:
		for i, child := range value {
			checked, err := inspectConfig(child, prefix)
			if err != nil {
				return nil, err
			}
			value[i] = checked
		}
	}
	return value, nil
}

func buildTags(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errors.New("expected run.build-tags to be a list of strings")
	}
	tags := make([]string, 0, len(list))
	for _, item := range list {
		tag, ok := item.(string)
		if !ok {
			return nil, errors.New("expected run.build-tags to be a list of strings")
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

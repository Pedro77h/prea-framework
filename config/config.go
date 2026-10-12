// Package config loads the application configuration, Spring style:
// application.yaml is the base and application-<profile>.yaml completes it
// for the active profile. The environment only enters where the yaml
// references it, with ${VAR}; the .env file is loaded first, without
// replacing variables already set.
//
//	database:
//	  url: ${DATABASE_URL}
//
// Components read values with the value tag, filled by the di container. A
// key that isn't in the yaml leaves the field as it is: zero, or nil. Values
// are converted to the field's type ("true" → bool, "30s" → time.Duration);
// blocks are decoded by their yaml tags.
//
//	type Mailer struct {
//		Addr string     `value:"mail.addr"`
//		Mail MailConfig `value:"mail"` // a whole block, decoded with yaml tags
//	}
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-viper/mapstructure/v2"
	"github.com/joho/godotenv"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const ProfileEnv = "PREA_PROFILE"

type Config struct {
	profile string
	k       *koanf.Koanf
}

// Load reads dir/application.yaml and dir/application-<profile>.yaml; a
// missing file is skipped. An empty profile comes from PREA_PROFILE.
func Load(dir, profile string) (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("config: .env: %w", err)
	}
	if profile == "" {
		profile = os.Getenv(ProfileEnv)
	}

	k := koanf.New(".")
	files := []string{"application.yaml"}
	if profile != "" {
		files = append(files, "application-"+profile+".yaml")
	}
	for _, name := range files {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("config: %s: %w", name, err)
		}
	}
	if err := expandEnv(k); err != nil {
		return nil, err
	}
	return &Config{profile, k}, nil
}

// New builds a config from a tree, without files or env; for tests.
func New(profile string, tree map[string]any) *Config {
	k := koanf.New(".")
	k.Load(confmap.Provider(tree, ""), nil)
	return &Config{profile, k}
}

// Profile is the active profile; "" when none.
func (c *Config) Profile() string {
	if c == nil {
		return ""
	}
	return c.profile
}

// Value decodes key into dst, a pointer. A key that isn't in the yaml (or is
// null) leaves dst as it is.
func (c *Config) Value(key string, dst any) error {
	if c.k.Get(key) == nil {
		return nil
	}
	err := c.k.UnmarshalWithConf(key, dst, koanf.UnmarshalConf{
		Tag: "yaml",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
			WeaklyTypedInput: true,
			Result:           dst,
			TagName:          "yaml",
		},
	})
	if err != nil {
		return fmt.Errorf("config: %s: %w", key, err)
	}
	return nil
}

var envRef = regexp.MustCompile(`\$\{(\w+)\}`)

// expandEnv replaces ${VAR} in the string values. A value that is only an
// unset ${VAR} is removed, like a key that isn't there.
func expandEnv(k *koanf.Koanf) error {
	expanded := map[string]any{}
	for key, v := range k.All() {
		s, ok := v.(string)
		if !ok || !envRef.MatchString(s) {
			continue
		}
		if m := envRef.FindStringSubmatch(s); m[0] == s {
			if _, set := os.LookupEnv(m[1]); !set {
				k.Delete(key)
				continue
			}
		}
		expanded[key] = envRef.ReplaceAllStringFunc(s, func(ref string) string {
			return os.Getenv(envRef.FindStringSubmatch(ref)[1])
		})
	}
	if err := k.Load(confmap.Provider(expanded, "."), nil); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

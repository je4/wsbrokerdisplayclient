package config

import (
	_ "embed"
	"time"

	"emperror.dev/errors"
	"github.com/BurntSushi/toml"
	"go.ub.unibas.ch/cloud/certloader/v2/pkg/loader"
)

//go:embed client.toml
var ClientConfigTOML string

// Config defines configuration parameters for the wsbrokerdisplayclient application.
type Config struct {
	Addr        string        `toml:"addr"`
	ExtAddr     string        `toml:"extaddr"`
	Name        string        `toml:"name"`
	WSBroker    string        `toml:"wsbroker"`
	Channels    []string      `toml:"channels"`
	Targets     []string      `toml:"targets"`
	MaxMessages int           `toml:"max_messages"`
	Timeout     time.Duration `toml:"timeout"`
	ReadLimit   int64         `toml:"read_limit"`
	ClientTLS   loader.Config `toml:"clienttls"`
	WebTLS      loader.Config `toml:"webtls"`
}

// LoadConfig loads the Config from the specified TOML file path, or falls back
// to the embedded default configuration (client.toml) if path is empty.
func LoadConfig(path string) (*Config, error) {
	conf := &Config{
		Addr:        "localhost:8770",
		ExtAddr:     "http://localhost:8770",
		Name:        "displayclient01",
		WSBroker:    "https://localhost:8765",
		Channels:    []string{"all", "display"},
		Targets:     []string{"display01", "display"},
		MaxMessages: 100,
		Timeout:     10 * time.Second,
		ReadLimit:   16 * 1024 * 1024,
	}
	if path == "" {
		if _, err := toml.Decode(ClientConfigTOML, conf); err != nil {
			return nil, errors.Wrap(err, "cannot decode embedded default config")
		}
		return conf, nil
	}
	if _, err := toml.DecodeFile(path, conf); err != nil {
		return nil, errors.Wrapf(err, "cannot decode toml file '%s'", path)
	}
	return conf, nil
}

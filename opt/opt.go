package opt

import (
	"flag"
	"fmt"
	"log"
	"sync"

	"github.com/spf13/viper"
)

var (
	config string
	Cfg    = &Config{}
)

type Config struct {
	mu              sync.RWMutex `mapstructure:"-"`
	Endpoint        string       `mapstructure:"endpoint"`
	AccessKey       string       `mapstructure:"access_key"`
	SecretAccessKey string       `mapstructure:"secret_access_key"`
	Cookie          string       `mapstructure:"cookie"`
	Addr            string       `mapstructure:"addr"`
	BucketName      string       `mapstructure:"bucket_name"`
}

func (c *Config) GetCookie() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Cookie
}

func (c *Config) UpdateCookie(cookie string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Cookie = cookie
	return c.Cookie
}

func init() {
	flag.StringVar(&config, "c", "etc/config.json", "")
	flag.Parse()
}

func MustInitConfig() error {
	viper.SetConfigType("json")
	viper.SetConfigFile(config)

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("read in config[%s] err: %w", config, err)
	}

	if err := viper.Unmarshal(Cfg); err != nil {
		return fmt.Errorf("unmarshal config err: %w", err)
	}

	log.Printf("config: %+v", Cfg)

	return nil
}

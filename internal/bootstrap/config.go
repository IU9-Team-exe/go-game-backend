package bootstrap

import (
	"github.com/spf13/viper"
)

type Config struct {
	ServerPort    string `mapstructure:"SERVER_PORT"`
	GpuServerIp   string `mapstructure:"GPU_SERVER_IP"`
	GpuServerPort string `mapstructure:"GPU_SERVER_PORT"`
	KatagoBotUrl  string `mapstructure:"KATAGO_BOT_URL"`
	RedisUrl      string `mapstructure:"REDIS_URL"`
}

func Setup(cfgPath string) (*Config, error) {
	viper.SetConfigFile(cfgPath)

	err := viper.ReadInConfig()
	if err != nil {
		return nil, err
	}

	var cfg Config

	err = viper.Unmarshal(&cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

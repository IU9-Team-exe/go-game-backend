package bootstrap

import (
	"github.com/spf13/viper"
)

type Config struct {
	ServerPort       string `mapstructure:"SERVER_PORT"`
	GpuServerIp      string `mapstructure:"GPU_SERVER_IP"`
	GpuServerPort    string `mapstructure:"GPU_SERVER_PORT"`
	KatagoBotUrl     string `mapstructure:"KATAGO_BOT_URL"`
	RedisUrl         string `mapstructure:"REDIS_URL"`
	MongoUri         string `mapstructure:"MONGO_URI"`
	IsLocalCors      bool   `mapstructure:"LOCAL_CORS"`
	PageLimitGames   int    `mapstructure:"PAGE_LIMIT_GAMES"`
	PageLimitPlayers int    `mapstructure:"PAGE_LIMIT_PLAYERS"`
	LlmApiKey        string `mapstructure:"LLM_API_KEY"`
	LlmAgentKey      string `mapstructure:"LLM_AGENT_KEY"`
}

func Setup(cfgPath string) (*Config, error) {
	viper.SetDefault("PAGE_LIMIT_GAMES", 20)
	viper.SetDefault("PAGE_LIMIT_PLAYERS", 30)
	viper.SetDefault("PAGE_LIMIT_TASKS", 10)
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

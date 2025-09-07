package config

import (
	"os"

	"gopkg.in/yaml.v2"
)

// 配置
type Config struct {
	Database  DatabaseConfig  `yaml:"database"`
	Chains    []ChainConfig   `yaml:"chains"`
	Processor ProcessorConfig `yaml:"processor"`
	Points    PointsConfig    `yaml:"points"`
}

// 数据库
type DatabaseConfig struct {
	Driver   string `yaml:"driver"`
	DSN      string `yaml:"dsn"`
	MaxOpen  int    `yaml:"max_open"`
	MaxIdle  int    `yaml:"max_idle"`
	LifeTime int    `yaml:"life_time"`
}

// 区块链
type ChainConfig struct {
	Name         string `yaml:"name"`
	ID           int64  `yaml:"id"`
	RPCURL       string `yaml:"rpc_url"`
	ContractAddr string `yaml:"contract_addr"`
	StartBlock   uint64 `yaml:"start_block"`
}

// 事件
type ProcessorConfig struct {
	BlockBatchSize uint64 `yaml:"block_batch_size"`
	ReorgThreshold uint64 `yaml:"reorg_threshold"`
	CheckInterval  int    `yaml:"check_interval"`
}

// 积分计算
type PointsConfig struct {
	CalculationInterval int     `yaml:"calculation_interval"`
	Rate                float64 `yaml:"rate"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

package config

import (
	"encoding/json"
	"os"
)

type SNMPConfig struct {
	Community string `json:"community"`
	Timeout   int    `json:"timeout_sec"`
	Retries   int    `json:"retries"`
}

type RefreshConfig struct {
	NormalIntervalSec int `json:"normal_interval_sec"`
	FastIntervalSec   int `json:"fast_interval_sec"`
	FastTimeoutSec    int `json:"fast_timeout_sec"`
	FastRetries       int `json:"fast_retries"`
}

type PDUDeviceConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	PortCount int  `json:"port_count"`
}

type ServerConfig struct {
	Addr string `json:"addr"`
}

type Config struct {
	Server  ServerConfig     `json:"server"`
	SNMP    SNMPConfig       `json:"snmp"`
	Refresh RefreshConfig    `json:"refresh"`
	Devices []PDUDeviceConfig `json:"devices"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Addr: ":8080",
		},
		SNMP: SNMPConfig{
			Community: "public",
			Timeout:   3,
			Retries:   1,
		},
		Refresh: RefreshConfig{
			NormalIntervalSec: 30,
			FastIntervalSec:   3,
			FastTimeoutSec:    1,
			FastRetries:       0,
		},
		Devices: []PDUDeviceConfig{
			{ID: "pdu-01", Name: "Cabinet-A PDU", Address: "192.168.1.100", PortCount: 8},
			{ID: "pdu-02", Name: "Cabinet-B PDU", Address: "192.168.1.101", PortCount: 8},
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			return cfg, cfg.Save(path)
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

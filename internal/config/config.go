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
	ID                string `json:"id"`
	Name              string `json:"name"`
	Address           string `json:"address"`
	PortCount         int    `json:"port_count"`
	DistributionBoxID string `json:"distribution_box_id"`
}

type DistributionBoxConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CorrelationConfig struct {
	TimeWindowSec   int     `json:"time_window_sec"`
	FaultRatio      float64 `json:"fault_ratio"`
	MinDevices      int     `json:"min_devices"`
	MaxEventAgeMin  int     `json:"max_event_age_min"`
}

type ServerConfig struct {
	Addr string `json:"addr"`
}

type Config struct {
	Server        ServerConfig           `json:"server"`
	SNMP          SNMPConfig             `json:"snmp"`
	Refresh       RefreshConfig          `json:"refresh"`
	Correlation   CorrelationConfig      `json:"correlation"`
	DistributionBoxes []DistributionBoxConfig `json:"distribution_boxes"`
	Devices       []PDUDeviceConfig      `json:"devices"`
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
		Correlation: CorrelationConfig{
			TimeWindowSec:  30,
			FaultRatio:     0.7,
			MinDevices:     2,
			MaxEventAgeMin: 60,
		},
		DistributionBoxes: []DistributionBoxConfig{
			{ID: "box-01", Name: "1#配电箱-A回路", Description: "机房A区 3楼机柜群 主回路"},
			{ID: "box-02", Name: "2#配电箱-B回路", Description: "机房B区 3楼机柜群 备回路"},
		},
		Devices: []PDUDeviceConfig{
			{ID: "pdu-01", Name: "Cabinet-A PDU", Address: "192.168.1.100", PortCount: 8, DistributionBoxID: "box-01"},
			{ID: "pdu-02", Name: "Cabinet-B PDU", Address: "192.168.1.101", PortCount: 8, DistributionBoxID: "box-01"},
			{ID: "pdu-03", Name: "Cabinet-C PDU", Address: "192.168.1.102", PortCount: 8, DistributionBoxID: "box-02"},
			{ID: "pdu-04", Name: "Cabinet-D PDU", Address: "192.168.1.103", PortCount: 8, DistributionBoxID: "box-02"},
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

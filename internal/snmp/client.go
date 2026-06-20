package snmp

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	OIDPortName       = "1.3.6.1.4.1.318.1.1.12.3.3.1.1.2"
	OIDPortStatus     = "1.3.6.1.4.1.318.1.1.12.3.5.1.1.4"
	OIDPortVoltage    = "1.3.6.1.4.1.318.1.1.12.3.5.1.1.6"
	OIDPortCurrent    = "1.3.6.1.4.1.318.1.1.12.3.5.1.1.7"
	OIDPortPower      = "1.3.6.1.4.1.318.1.1.12.3.5.1.1.9"
	OIDDeviceTotalPower = "1.3.6.1.4.1.318.1.1.12.1.16"
)

type Client struct {
	handler *gosnmp.GoSNMP
}

func NewClient(address, community string, timeoutSec, retries int) *Client {
	handler := &gosnmp.GoSNMP{
		Target:    address,
		Port:      161,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Duration(timeoutSec) * time.Second,
		Retries:   retries,
	}
	return &Client{handler: handler}
}

func (c *Client) Connect() error {
	return c.handler.Connect()
}

func (c *Client) Close() {
	c.handler.Conn.Close()
}

func (c *Client) IsReachable() bool {
	err := c.handler.Connect()
	if err != nil {
		return false
	}
	defer c.handler.Conn.Close()
	_, err = c.handler.Get([]string{"1.3.6.1.2.1.1.1.0"})
	return err == nil
}

func (c *Client) Walk(oid string) (map[string]interface{}, error) {
	if err := c.handler.Connect(); err != nil {
		return nil, fmt.Errorf("snmp connect failed: %w", err)
	}
	defer c.handler.Conn.Close()

	results := make(map[string]interface{})
	err := c.handler.Walk(oid, func(pdu gosnmp.SnmpPDU) error {
		results[pdu.Name] = pdu.Value
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("snmp walk %s failed: %w", oid, err)
	}
	return results, nil
}

func (c *Client) Get(oids []string) (map[string]interface{}, error) {
	if err := c.handler.Connect(); err != nil {
		return nil, fmt.Errorf("snmp connect failed: %w", err)
	}
	defer c.handler.Conn.Close()

	result, err := c.handler.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("snmp get failed: %w", err)
	}
	values := make(map[string]interface{})
	for _, pdu := range result.Variables {
		values[pdu.Name] = pdu.Value
	}
	return values, nil
}

func ToFloat(val interface{}) float64 {
	switch v := val.(type) {
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	case string:
		return 0
	default:
		return 0
	}
}

func ToInt(val interface{}) int {
	return int(ToFloat(val))
}

func ToString(val interface{}) string {
	if b, ok := val.([]byte); ok {
		return string(b)
	}
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

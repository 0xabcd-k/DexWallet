package background

import (
	"fmt"
	"time"
)

type Config struct {
	Publishers       int           `json:",default=2"`
	Workers          int           `json:",default=2"`
	PollInterval     time.Duration `json:",default=1s"`
	OperationTimeout time.Duration `json:",default=10s"`
	LeaseDuration    time.Duration `json:",default=30s"`
	RetryBase        time.Duration `json:",default=5s"`
	RetryMax         time.Duration `json:",default=5m"`
	MaxAttempts      int64         `json:",default=6"`
}

func Defaults() Config {
	return Config{2, 2, time.Second, 10 * time.Second, 30 * time.Second, 5 * time.Second, 5 * time.Minute, 6}
}
func (c Config) Check() error {
	if c.Publishers < 1 || c.Publishers > 32 || c.Workers < 1 || c.Workers > 32 || c.PollInterval <= 0 || c.OperationTimeout <= 0 || c.LeaseDuration <= c.OperationTimeout || c.RetryBase <= 0 || c.RetryMax < c.RetryBase || c.RetryMax > 24*time.Hour || c.MaxAttempts < 1 || c.MaxAttempts > 100 {
		return fmt.Errorf("invalid background concurrency, timeout, lease or retry policy")
	}
	return nil
}

func (c Config) delay(attempt int64) time.Duration {
	delay := c.RetryBase
	for i := int64(1); i < attempt; i++ {
		if delay >= c.RetryMax/2 {
			return c.RetryMax
		}
		delay *= 2
	}
	return min(delay, c.RetryMax)
}

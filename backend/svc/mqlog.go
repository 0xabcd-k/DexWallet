package svc

import (
	"fmt"
	"sync"

	"github.com/apache/rocketmq-client-go/v2/rlog"
	"github.com/zeromicro/go-zero/core/logx"
)

var mqLogOnce sync.Once

// SDK diagnostics use the same structured sink as application diagnostics.
type mqLogger struct{}

func mqFields(fields map[string]interface{}) []logx.LogField {
	result := []logx.LogField{logx.Field("dependency", MQDependency)}
	for k, v := range fields {
		result = append(result, logx.Field(k, v))
	}
	return result
}
func (mqLogger) Debug(s string, f map[string]interface{})   { logx.Debugw(s, mqFields(f)...) }
func (mqLogger) Info(s string, f map[string]interface{})    { logx.Infow(s, mqFields(f)...) }
func (mqLogger) Warning(s string, f map[string]interface{}) { logx.Sloww(s, mqFields(f)...) }
func (mqLogger) Error(s string, f map[string]interface{})   { logx.Errorw(s, mqFields(f)...) }
func (mqLogger) Fatal(s string, f map[string]interface{}) {
	logx.Errorw(s, mqFields(f)...)
	panic(fmt.Sprintf("rocketmq fatal: %s", s))
}
func (mqLogger) Level(string)            {}
func (mqLogger) OutputPath(string) error { return fmt.Errorf("MQ logs use application log output") }
func setupMQLogging()                    { mqLogOnce.Do(func() { rlog.SetLogger(mqLogger{}) }) }

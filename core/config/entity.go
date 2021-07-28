// Copyright 1999-2020 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"encoding/json"
	"fmt"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/pkg/errors"
)

type Entity struct {
	// Version represents the format version of the entity.
	Version string

	Sentinel SentinelConfig
}

// SentinelConfig represent the general configuration of Sentinel.
// 表示Sentinel的一般配置。
type SentinelConfig struct {
	App struct {
		// Name represents the name of current running service.
		// 表示当前正在运行的服务的名称。 必填
		Name string
		// Type indicates the classification of the service (e.g. web service, API gateway).
		// 表示服务的分类(例如web服务、API网关)。
		Type int32
	}
	// Log represents configuration items related to logging.
	// 与日志相关的配置项。
	Log LogConfig
	// Stat represents configuration items related to statistics.
	Stat StatConfig
	// UseCacheTime indicates whether to cache time(ms)
	// 是否通过异步协程缓存时间 (ms)
	UseCacheTime bool `yaml:"useCacheTime"`
}

// LogConfig represent the configuration of logging in Sentinel.
// 表示Sentinel中的日志配置。
type LogConfig struct {
	// Logger indicates that using logger to replace default logging.
	Logger logging.Logger
	// Dir represents the log directory path.
	Dir string
	// UsePid indicates whether the filename ends with the process ID (PID).
	// 监控日志文件名是否带上进程 PID
	UsePid bool `yaml:"usePid"`
	// Metric represents the configuration items of the metric log.
	Metric MetricLogConfig
}

// MetricLogConfig represents the configuration items of the metric log.
// 表示度量日志的配置项。
type MetricLogConfig struct {
	// 监控日志单个文件大小上限
	SingleFileMaxSize uint64 `yaml:"singleFileMaxSize"`
	// 监控日志最大文件数目
	MaxFileCount      uint32 `yaml:"maxFileCount"`
	// 监控日志聚合和刷盘的时间频率
	FlushIntervalSec  uint32 `yaml:"flushIntervalSec"`
}

// StatConfig represents the configuration items of statistics.
type StatConfig struct {
	// GlobalStatisticSampleCountTotal and GlobalStatisticIntervalMsTotal is the per resource's global default statistic sliding window config
	// Resource的全局滑动窗口的统计格子数
	GlobalStatisticSampleCountTotal uint32 `yaml:"globalStatisticSampleCountTotal"`
	// Resource的全局滑动窗口的间隔时间(ms)
	GlobalStatisticIntervalMsTotal  uint32 `yaml:"globalStatisticIntervalMsTotal"`

	// MetricStatisticSampleCount and MetricStatisticIntervalMs is the per resource's default readonly metric statistic
	// This default readonly metric statistic must be reusable based on global statistic.
	// Resource的默认监控日志统计的滑动窗口的统计格子数
	MetricStatisticSampleCount uint32 `yaml:"metricStatisticSampleCount"`
	// Resource的默认监控日志统计的滑动窗口的间隔时间
	MetricStatisticIntervalMs  uint32 `yaml:"metricStatisticIntervalMs"`

	System SystemStatConfig `yaml:"system"`
}

// SystemStatConfig represents the configuration items of system statistics.
// 系统统计的配置项。
type SystemStatConfig struct {
	// CollectIntervalMs represents the collecting interval of the system metrics collector.
	// 系统指标收集的间隔时间
	CollectIntervalMs uint32 `yaml:"collectIntervalMs"`
	// CollectLoadIntervalMs represents the collecting interval of the system load collector.
	// 表示系统负载收集器的收集间隔。
	CollectLoadIntervalMs uint32 `yaml:"collectLoadIntervalMs"`
	// CollectCpuIntervalMs represents the collecting interval of the system cpu usage collector.
	// 表示系统CPU利用率采集器的采集周期。
	CollectCpuIntervalMs uint32 `yaml:"collectCpuIntervalMs"`
	// CollectMemoryIntervalMs represents the collecting interval of the system memory usage collector.
	// 表示系统内存利用率采集器的采集周期。
	CollectMemoryIntervalMs uint32 `yaml:"collectMemoryIntervalMs"`
}

// NewDefaultConfig creates a new default config entity.
// 创建默认的配置
func NewDefaultConfig() *Entity {
	return &Entity{
		Version: "v1",
		Sentinel: SentinelConfig{
			App: struct {
				Name string
				Type int32
			}{
				Name: UnknownProjectName,
				Type: DefaultAppType,
			},
			Log: LogConfig{
				Logger: nil,
				Dir:    GetDefaultLogDir(),
				UsePid: false,
				Metric: MetricLogConfig{
					SingleFileMaxSize: DefaultMetricLogSingleFileMaxSize,
					MaxFileCount:      DefaultMetricLogMaxFileAmount,
					FlushIntervalSec:  DefaultMetricLogFlushIntervalSec,
				},
			},
			Stat: StatConfig{
				GlobalStatisticSampleCountTotal: base.DefaultSampleCountTotal,
				GlobalStatisticIntervalMsTotal:  base.DefaultIntervalMsTotal,
				MetricStatisticSampleCount:      base.DefaultSampleCount,
				MetricStatisticIntervalMs:       base.DefaultIntervalMs,
				System: SystemStatConfig{
					CollectIntervalMs:       DefaultSystemStatCollectIntervalMs,
					CollectLoadIntervalMs:   DefaultLoadStatCollectIntervalMs,
					CollectCpuIntervalMs:    DefaultCpuStatCollectIntervalMs,
					CollectMemoryIntervalMs: DefaultMemoryStatCollectIntervalMs,
				},
			},
			UseCacheTime: true,
		},
	}
}

// CheckValid 校验整个entity配置
func CheckValid(entity *Entity) error {
	if entity == nil {
		return errors.New("Nil entity")
	}
	if len(entity.Version) == 0 {
		return errors.New("Empty version")
	}
	return checkConfValid(&entity.Sentinel)
}

// 校验配置项
func checkConfValid(conf *SentinelConfig) error {
	if conf == nil {
		return errors.New("Nil globalCfg")
	}
	if conf.App.Name == "" {
		return errors.New("App.Name is empty")
	}
	mc := conf.Log.Metric
	if mc.MaxFileCount <= 0 {
		return errors.New("Illegal metric log globalCfg: maxFileCount <= 0")
	}
	if mc.SingleFileMaxSize <= 0 {
		return errors.New("Illegal metric log globalCfg: singleFileMaxSize <= 0")
	}
	if err := base.CheckValidityForReuseStatistic(conf.Stat.MetricStatisticSampleCount, conf.Stat.MetricStatisticIntervalMs,
		conf.Stat.GlobalStatisticSampleCountTotal, conf.Stat.GlobalStatisticIntervalMsTotal); err != nil {
		return err
	}
	return nil
}

func (entity *Entity) String() string {
	e, err := json.Marshal(entity)
	if err != nil {
		return fmt.Sprintf("%+v", *entity)
	}
	return string(e)
}

func (entity *Entity) AppName() string {
	return entity.Sentinel.App.Name
}

func (entity *Entity) AppType() int32 {
	return entity.Sentinel.App.Type
}

// LogBaseDir log目录
func (entity *Entity) LogBaseDir() string {
	return entity.Sentinel.Log.Dir
}

// Logger 如果用户期望使用自定义的Logger去覆盖Sentinel默认的logger，
// 需要指定这个对象，如果指定了，Sentinel就会使用这个对象打日志
func (entity *Entity) Logger() logging.Logger {
	return entity.Sentinel.Log.Logger
}

// LogUsePid returns whether the log file name contains the PID suffix.
func (entity *Entity) LogUsePid() bool {
	return entity.Sentinel.Log.UsePid
}

func (entity *Entity) MetricLogFlushIntervalSec() uint32 {
	return entity.Sentinel.Log.Metric.FlushIntervalSec
}

func (entity *Entity) MetricLogSingleFileMaxSize() uint64 {
	return entity.Sentinel.Log.Metric.SingleFileMaxSize
}

func (entity *Entity) MetricLogMaxFileAmount() uint32 {
	return entity.Sentinel.Log.Metric.MaxFileCount
}

func (entity *Entity) SystemStatCollectIntervalMs() uint32 {
	return entity.Sentinel.Stat.System.CollectIntervalMs
}

func (entity *Entity) LoadStatCollectIntervalMs() uint32 {
	return entity.Sentinel.Stat.System.CollectLoadIntervalMs
}

func (entity *Entity) CpuStatCollectIntervalMs() uint32 {
	return entity.Sentinel.Stat.System.CollectCpuIntervalMs
}

func (entity *Entity) MemoryStatCollectIntervalMs() uint32 {
	return entity.Sentinel.Stat.System.CollectMemoryIntervalMs
}

func (entity *Entity) UseCacheTime() bool {
	return entity.Sentinel.UseCacheTime
}

func (entity *Entity) GlobalStatisticIntervalMsTotal() uint32 {
	return entity.Sentinel.Stat.GlobalStatisticIntervalMsTotal
}

func (entity *Entity) GlobalStatisticSampleCountTotal() uint32 {
	return entity.Sentinel.Stat.GlobalStatisticSampleCountTotal
}

func (entity *Entity) MetricStatisticIntervalMs() uint32 {
	return entity.Sentinel.Stat.MetricStatisticIntervalMs
}
func (entity *Entity) MetricStatisticSampleCount() uint32 {
	return entity.Sentinel.Stat.MetricStatisticSampleCount
}

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
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

var (
	globalCfg   = NewDefaultConfig() // 全局配置
	initLogOnce sync.Once
)

// ResetGlobalConfig 重置全局配置
func ResetGlobalConfig(config *Entity) {
	globalCfg = config
}

// InitConfigWithYaml loads general configuration from the YAML file under provided path.
// 在提供的路径下从YAML文件加载通用配置。
func InitConfigWithYaml(filePath string) (err error) {
	// Initialize general config and logging module.
	// 初始化通用配置和日志记录模块。
	if err = applyYamlConfigFile(filePath); err != nil {
		return err
	}
	return OverrideConfigFromEnvAndInitLog()
}

// applyYamlConfigFile loads general configuration from the given YAML file.
// 从给定的YAML文件加载通用配置。
func applyYamlConfigFile(configPath string) error {
	// Priority: system environment > YAML file > default config
	// 环境变量大于手动配置。
	if util.IsBlank(configPath) {
		// If the config file path is absent, Sentinel will try to resolve it from the system env.
		// 如果配置文件路径不存在，Sentinel将尝试从系统env解析它。
		configPath = os.Getenv(ConfFilePathEnvKey)
	}
	// env未配置 使用默认配置文件
	if util.IsBlank(configPath) {
		configPath = DefaultConfigFilename
	}
	// First Sentinel will try to load config from the given file.
	// If the path is empty (not set), Sentinel will use the default config.
	// 尝试从给定的文件加载配置。如果路径为空(未设置)，Sentinel将使用默认配置。
	return loadGlobalConfigFromYamlFile(configPath)
}

// OverrideConfigFromEnvAndInitLog 重置配置信息值 初始化log
func OverrideConfigFromEnvAndInitLog() error {
	// Then Sentinel will try to get fundamental config items from system environment.
	// If present, the value in system env will override the value in config file.
	// 然后Sentinel将尝试从系统环境中获取基本配置项。如果存在，系统env中的值将覆盖配置文件中的值。
	err := overrideItemsFromSystemEnv()
	if err != nil {
		return err
	}

	defer logging.Info("[Config] Print effective global config", "globalConfig", *globalCfg)
	// Configured Logger is the highest priority
	if configLogger := Logger(); configLogger != nil {
		err = logging.ResetGlobalLogger(configLogger) // 重置logger
		if err != nil {
			return err
		}
		return nil
	}

	// 获取log目录
	logDir := LogBaseDir()
	if len(logDir) == 0 {
		logDir = GetDefaultLogDir()
	}

	// 初始化logger
	if err := initializeLogConfig(logDir, LogUsePid()); err != nil {
		return err
	}
	logging.Info("[Config] App name resolved", "appName", AppName())
	return nil
}

// 从文件加载配置文件
func loadGlobalConfigFromYamlFile(filePath string) error {
	if filePath == DefaultConfigFilename {
		if _, err := os.Stat(DefaultConfigFilename); err != nil {
			//use default globalCfg.
			return nil
		}
	}
	_, err := os.Stat(filePath)
	if err != nil && !os.IsExist(err) {
		return err
	}
	// 读取 解析配置内容
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(content, globalCfg)
	if err != nil {
		return err
	}
	logging.Info("[Config] Resolving Sentinel config from file", "file", filePath)
	return checkConfValid(&(globalCfg.Sentinel))
}

// 配置项重置
func overrideItemsFromSystemEnv() error {
	// app名称
	if appName := os.Getenv(AppNameEnvKey); !util.IsBlank(appName) {
		globalCfg.Sentinel.App.Name = appName
	}

	// app类型
	if appTypeStr := os.Getenv(AppTypeEnvKey); !util.IsBlank(appTypeStr) {
		appType, err := strconv.ParseInt(appTypeStr, 10, 32)
		if err != nil {
			return err
		} else {
			globalCfg.Sentinel.App.Type = int32(appType)
		}
	}

	// 监控日志文件名是否带上进程 PID
	if addPidStr := os.Getenv(LogNamePidEnvKey); !util.IsBlank(addPidStr) {
		addPid, err := strconv.ParseBool(addPidStr)
		if err != nil {
			return err
		} else {
			globalCfg.Sentinel.Log.UsePid = addPid
		}
	}

	// 日志目录
	if logDir := os.Getenv(LogDirEnvKey); !util.IsBlank(logDir) {
		globalCfg.Sentinel.Log.Dir = logDir
	}

	// 重新检查配置
	return checkConfValid(&(globalCfg.Sentinel))
}

// 初始化logger
func initializeLogConfig(logDir string, usePid bool) (err error) {
	if logDir == "" {
		return errors.New("invalid empty log path")
	}

	initLogOnce.Do(func() {
		// 创建日志目录
		if err = util.CreateDirIfNotExists(logDir); err != nil {
			return
		}
		err = reconfigureRecordLogger(logDir, usePid)
	})
	return err
}

// 设置默认的logger
func reconfigureRecordLogger(logBaseDir string, withPid bool) error {
	// 获取记录日志的文件名
	filePath := filepath.Join(logBaseDir, logging.RecordLogFileName)
	if withPid {
		filePath = filePath + ".pid" + strconv.Itoa(os.Getpid())
	}

	// 创建并设置logger
	fileLogger, err := logging.NewSimpleFileLogger(filePath)
	if err != nil {
		return err
	}
	// Note: not thread-safe!
	if err := logging.ResetGlobalLogger(fileLogger); err != nil {
		return err
	}

	logging.Info("[Config] Log base directory", "baseDir", logBaseDir)

	return nil
}

// GetDefaultLogDir 获取默认的日志目录
func GetDefaultLogDir() string {
	home, err := os.UserHomeDir() // home目录
	if err != nil {
		return ""
	}
	return filepath.Join(home, logging.DefaultDirName)
}

func AppName() string {
	return globalCfg.AppName()
}

func AppType() int32 {
	return globalCfg.AppType()
}

func Logger() logging.Logger {
	return globalCfg.Logger()
}

func LogBaseDir() string {
	return globalCfg.LogBaseDir()
}

// LogUsePid returns whether the log file name contains the PID suffix.
func LogUsePid() bool {
	return globalCfg.LogUsePid()
}

func MetricLogFlushIntervalSec() uint32 {
	return globalCfg.MetricLogFlushIntervalSec()
}

func MetricLogSingleFileMaxSize() uint64 {
	return globalCfg.MetricLogSingleFileMaxSize()
}

func MetricLogMaxFileAmount() uint32 {
	return globalCfg.MetricLogMaxFileAmount()
}

func SystemStatCollectIntervalMs() uint32 {
	return globalCfg.SystemStatCollectIntervalMs()
}

func LoadStatCollectIntervalMs() uint32 {
	return globalCfg.LoadStatCollectIntervalMs()
}

func CpuStatCollectIntervalMs() uint32 {
	return globalCfg.CpuStatCollectIntervalMs()
}

func MemoryStatCollectIntervalMs() uint32 {
	return globalCfg.MemoryStatCollectIntervalMs()
}

func UseCacheTime() bool {
	return globalCfg.UseCacheTime()
}

func GlobalStatisticIntervalMsTotal() uint32 {
	return globalCfg.GlobalStatisticIntervalMsTotal()
}

func GlobalStatisticSampleCountTotal() uint32 {
	return globalCfg.GlobalStatisticSampleCountTotal()
}

func GlobalStatisticBucketLengthInMs() uint32 {
	return globalCfg.GlobalStatisticIntervalMsTotal() / GlobalStatisticSampleCountTotal()
}

func MetricStatisticIntervalMs() uint32 {
	return globalCfg.MetricStatisticIntervalMs()
}
func MetricStatisticSampleCount() uint32 {
	return globalCfg.MetricStatisticSampleCount()
}

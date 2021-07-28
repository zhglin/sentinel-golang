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

const (
	// UnknownProjectName represents the "default" value
	// that indicates the project name is absent.
	UnknownProjectName = "unknown_go_service"

	// env配置
	ConfFilePathEnvKey = "SENTINEL_CONFIG_FILE_PATH"
	AppNameEnvKey      = "SENTINEL_APP_NAME"	// app名称
	AppTypeEnvKey      = "SENTINEL_APP_TYPE"	// app类型
	LogDirEnvKey       = "SENTINEL_LOG_DIR"		// 日志目录
	LogNamePidEnvKey   = "SENTINEL_LOG_USE_PID" // 监控日志文件名是否带上进程 PID

	DefaultConfigFilename       = "sentinel.yml"
	DefaultAppType        int32 = 0

	DefaultMetricLogFlushIntervalSec   uint32 = 1
	DefaultMetricLogSingleFileMaxSize  uint64 = 1024 * 1024 * 50
	DefaultMetricLogMaxFileAmount      uint32 = 8
	DefaultSystemStatCollectIntervalMs uint32 = 1000
	DefaultLoadStatCollectIntervalMs   uint32 = 1000
	DefaultCpuStatCollectIntervalMs    uint32 = 1000
	DefaultMemoryStatCollectIntervalMs uint32 = 150
	DefaultWarmUpColdFactor            uint32 = 3
)

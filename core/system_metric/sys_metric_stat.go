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

package system_metric

import (
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/metrics"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

const (
	NotRetrievedLoadValue     float64 = -1.0
	NotRetrievedCpuUsageValue float64 = -1.0
	NotRetrievedMemoryValue   int64   = -1
)

var (
	currentLoad        atomic.Value
	currentCpuUsage    atomic.Value
	currentMemoryUsage atomic.Value

	loadStatCollectorOnce   sync.Once
	memoryStatCollectorOnce sync.Once
	cpuStatCollectorOnce    sync.Once

	CurrentPID         = os.Getpid() // 当前进程的pid
	currentProcess     atomic.Value
	currentProcessOnce sync.Once
	TotalMemorySize    = getTotalMemorySize()

	ssStopChan = make(chan struct{})
)

func init() {
	// 初始化load cpu memory采集存储
	currentLoad.Store(NotRetrievedLoadValue)
	currentCpuUsage.Store(NotRetrievedCpuUsageValue)
	currentMemoryUsage.Store(NotRetrievedMemoryValue)

	p, err := process.NewProcess(int32(CurrentPID))
	if err != nil {
		logging.Error(err, "Fail to new process when initializing system metric", "pid", CurrentPID)
		return
	}
	currentProcessOnce.Do(func() {
		currentProcess.Store(p)
	})
}

// getMemoryStat returns the current machine's memory statistic
// 返回当前机器的内存统计信息
func getTotalMemorySize() (total uint64) {
	stat, err := mem.VirtualMemory()
	if err != nil {
		logging.Error(err, "Fail to read Virtual Memory")
		return 0
	}
	return stat.Total
}

// InitMemoryCollector 初始化并定时更新当前进程的内存消耗量
func InitMemoryCollector(intervalMs uint32) {
	if intervalMs == 0 {
		return
	}
	memoryStatCollectorOnce.Do(func() {
		// Initial memory retrieval.
		retrieveAndUpdateMemoryStat()

		// 定时更新
		ticker := util.NewTicker(time.Duration(intervalMs) * time.Millisecond)
		go util.RunWithRecover(func() {
			for {
				select {
				case <-ticker.C():
					retrieveAndUpdateMemoryStat()
				case <-ssStopChan:
					ticker.Stop()
					return
				}
			}
		})
	})
}

// 获取并设置当前进程的内存的固定消耗量
func retrieveAndUpdateMemoryStat() {
	memoryUsedBytes, err := GetProcessMemoryStat()
	if err != nil {
		logging.Error(err, "Fail to retrieve and update cpu statistic")
		return
	}
	metrics.SetProcessMemorySize(memoryUsedBytes)
	currentMemoryUsage.Store(memoryUsedBytes)
}

// GetProcessMemoryStat gets current process's memory usage in Bytes
// 获取当前进程的固定内存消耗
func GetProcessMemoryStat() (int64, error) {
	curProcess := currentProcess.Load()
	if curProcess == nil {
		p, err := process.NewProcess(int32(CurrentPID))
		if err != nil {
			return 0, err
		}
		currentProcessOnce.Do(func() {
			currentProcess.Store(p)
		})
		curProcess = currentProcess.Load()
	}
	p := curProcess.(*process.Process)
	memInfo, err := p.MemoryInfo()
	var rss int64
	if memInfo != nil {
		rss = int64(memInfo.RSS) // 固定的内存消耗
	}

	return rss, err
}

// InitCpuCollector 初始化并定时更新当前进程的cpu利用率
func InitCpuCollector(intervalMs uint32) {
	if intervalMs == 0 {
		return
	}
	cpuStatCollectorOnce.Do(func() {
		// Initial memory retrieval.
		retrieveAndUpdateCpuStat()

		// 定时更新
		ticker := util.NewTicker(time.Duration(intervalMs) * time.Millisecond)
		go util.RunWithRecover(func() {
			for {
				select {
				case <-ticker.C():
					retrieveAndUpdateCpuStat()
				case <-ssStopChan:
					ticker.Stop()
					return
				}
			}
		})
	})
}

// 获取并设置cpu的利用率
func retrieveAndUpdateCpuStat() {
	cpuPercent, err := getProcessCpuStat()
	if err != nil {
		logging.Error(err, "Fail to retrieve and update cpu statistic")
		return
	}
	metrics.SetCPURatio(cpuPercent)
	currentCpuUsage.Store(cpuPercent)
}

// getProcessCpuStat gets current process's memory usage in Bytes
// 当前pid的cpu利用率
func getProcessCpuStat() (float64, error) {
	curProcess := currentProcess.Load()
	if curProcess == nil {
		p, err := process.NewProcess(int32(CurrentPID))
		if err != nil {
			return 0, err
		}
		currentProcessOnce.Do(func() {
			currentProcess.Store(p)
		})
		curProcess = currentProcess.Load()
	}
	p := curProcess.(*process.Process)
	return p.Percent(0)
}

// InitLoadCollector 定时更新一分钟的load值
func InitLoadCollector(intervalMs uint32) {
	if intervalMs == 0 {
		return
	}
	loadStatCollectorOnce.Do(func() {
		// Initial retrieval.
		// 初始化一次
		retrieveAndUpdateLoadStat()

		// 定时更新
		ticker := util.NewTicker(time.Duration(intervalMs) * time.Millisecond)
		go util.RunWithRecover(func() {
			for {
				select {
				case <-ticker.C():
					retrieveAndUpdateLoadStat()
				case <-ssStopChan:
					ticker.Stop()
					return
				}
			}
		})
	})
}

// 获取一分钟的load值
func retrieveAndUpdateLoadStat() {
	loadStat, err := load.Avg()
	if err != nil {
		logging.Error(err, "[retrieveAndUpdateSystemStat] Failed to retrieve current system load")
		return
	}
	if loadStat != nil {
		currentLoad.Store(loadStat.Load1)
	}
}

func CurrentLoad() float64 {
	r, ok := currentLoad.Load().(float64)
	if !ok {
		return NotRetrievedLoadValue
	}
	return r
}

// Note: SetSystemLoad is used for unit test, the user shouldn't call this function.
func SetSystemLoad(load float64) {
	currentLoad.Store(load)
}

func CurrentCpuUsage() float64 {
	r, ok := currentCpuUsage.Load().(float64)
	if !ok {
		return NotRetrievedCpuUsageValue
	}
	return r
}

// Note: SetSystemCpuUsage is used for unit test, the user shouldn't call this function.
func SetSystemCpuUsage(cpuUsage float64) {
	currentCpuUsage.Store(cpuUsage)
}

func CurrentMemoryUsage() int64 {
	bytes, ok := currentMemoryUsage.Load().(int64)
	if !ok {
		return NotRetrievedMemoryValue
	}

	return bytes
}

// Note: SetSystemCpuUsage is used for unit test, the user shouldn't call this function.
func SetSystemMemoryUsage(memoryUsage int64) {
	currentMemoryUsage.Store(memoryUsage)
}

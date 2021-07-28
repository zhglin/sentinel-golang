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

package metric

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alibaba/sentinel-golang/core/base"
)

const (
	// MetricFileNameSuffix represents the suffix of the metric file.
	MetricFileNameSuffix = "metrics.log"
	// MetricIdxSuffix represents the suffix of the metric index file.
	MetricIdxSuffix = ".idx"
	// FileLockSuffix represents the suffix of the lock file.
	FileLockSuffix = ".lck"
	// FilePidPrefix represents the pid flag of filename.
	FilePidPrefix = "pid"

	metricFilePattern = `\.[0-9]{4}-[0-9]{2}-[0-9]{2}(\.[0-9]*)?` // 日期匹配规则
)

var metricFileRegex = regexp.MustCompile(metricFilePattern)

// MetricLogWriter writes and flushes metric items to current metric log.
type MetricLogWriter interface {
	Write(ts uint64, items []*base.MetricItem) error
}

// MetricSearcher searches metric items from the metric log file under given condition.
type MetricSearcher interface {
	FindByTimeAndResource(beginTimeMs uint64, endTimeMs uint64, resource string) ([]*base.MetricItem, error)

	FindFromTimeWithMaxLines(beginTimeMs uint64, maxLines uint32) ([]*base.MetricItem, error)
}

// FormMetricFileName Generate the metric file name from the service name.
// 从服务名称生成监控文件名称。
func FormMetricFileName(serviceName string, withPid bool) string {
	// serviceName中.转成-
	dot := "."
	separator := "-"
	if strings.Contains(serviceName, dot) {
		serviceName = strings.ReplaceAll(serviceName, dot, separator)
	}

	// 文件名
	filename := serviceName + separator + MetricFileNameSuffix
	if withPid {
		pid := os.Getpid()
		filename = filename + ".pid" + strconv.Itoa(pid)
	}
	return filename
}

// Generate the metric index filename from the metric log filename.
// 从度量日志文件名生成度量索引文件名。
func formMetricIdxFileName(metricFilename string) string {
	return metricFilename + MetricIdxSuffix
}

// 文件名正则匹配筛选
func filenameMatches(filename, baseFilename string) bool {
	if !strings.HasPrefix(filename, baseFilename) {
		return false
	}
	part := filename[len(baseFilename):]
	// part is like: ".yyyy-MM-dd.number", eg. ".2018-12-24.11"
	// 匹配后面的日期
	return metricFileRegex.MatchString(part)
}

// 从baseDir目录中根据predicate函数判断是否存在filePattern的文件
func listMetricFilesConditional(baseDir string, filePattern string, predicate func(string, string) bool) ([]string, error) {
	dir, err := ioutil.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}
	arr := make([]string, 0, len(dir))
	for _, f := range dir {
		if f.IsDir() {  // 跳过目录
			continue
		}
		name := f.Name() // 文件名
		if predicate(name, filePattern) && !strings.HasSuffix(name, MetricIdxSuffix) && !strings.HasSuffix(name, FileLockSuffix) {
			// Put the absolute path into the slice.
			arr = append(arr, filepath.Join(baseDir, name))
		}
	}
	if len(arr) > 1 {
		sort.Slice(arr, filenameComparator(arr)) // 多个文件进行排序
	}
	return arr, nil
}

// List metrics files
// baseDir: the directory of metrics files
// filePattern: metric file pattern
// 在baseDir中按前缀匹配文件名
func listMetricFiles(baseDir, filePattern string) ([]string, error) {
	return listMetricFilesConditional(baseDir, filePattern, filenameMatches)
}

// 对匹配到的文件进行排序 只对最后的日期进行排序 有pid的跟没pid的有区别
func filenameComparator(arr []string) func(i, j int) bool {
	return func(i, j int) bool {
		name1 := filepath.Base(arr[i])
		name2 := filepath.Base(arr[j])
		a1 := strings.Split(name1, `.`)
		a2 := strings.Split(name2, `.`)
		dateStr1 := a1[2]
		dateStr2 := a2[2]

		// in case of file name contains pid, skip it, like Sentinel-Admin-metrics.log.pid22568.2018-12-24
		if strings.HasPrefix(a1[2], FilePidPrefix) {
			dateStr1 = a1[3]
			dateStr2 = a2[3]
		}

		// compare date first
		if dateStr1 != dateStr2 {
			return dateStr1 < dateStr2
		}

		// same date, compare the file number
		t := len(name1) - len(name2)
		if t != 0 {
			return t < 0
		}
		return name1 < name2
	}
}

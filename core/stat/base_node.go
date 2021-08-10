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

package stat

import (
	"sync/atomic"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/config"
	sbase "github.com/alibaba/sentinel-golang/core/stat/base"
)

type BaseStatNode struct {
	sampleCount uint32
	intervalMs  uint32

	concurrency int32 // 获取到资源的请求数

	arr    *sbase.BucketLeapArray     // 数据的写入
	metric *sbase.SlidingWindowMetric // arr的读取以及统计计算
}

// NewBaseStatNode
// sampleCount=config.MetricStatisticSampleCount(), intervalInMs=config.MetricStatisticIntervalMs()
// 这里是根据配置文件生成的每个resourceName的全局统计信息
// la,metric读写分开，利于重用
func NewBaseStatNode(sampleCount uint32, intervalInMs uint32) *BaseStatNode {
	la := sbase.NewBucketLeapArray(config.GlobalStatisticSampleCountTotal(), config.GlobalStatisticIntervalMsTotal())
	metric, _ := sbase.NewSlidingWindowMetric(sampleCount, intervalInMs, la)
	return &BaseStatNode{
		concurrency: 0,
		sampleCount: sampleCount,
		intervalMs:  intervalInMs,
		arr:         la,     // 写入
		metric:      metric, // 读取
	}
}

func (n *BaseStatNode) MetricsOnCondition(predicate base.TimePredicate) []*base.MetricItem {
	return n.metric.SecondMetricsOnCondition(predicate)
}

func (n *BaseStatNode) GetQPS(event base.MetricEvent) float64 {
	return n.metric.GetQPS(event)
}

func (n *BaseStatNode) GetPreviousQPS(event base.MetricEvent) float64 {
	return n.metric.GetPreviousQPS(event)
}

func (n *BaseStatNode) GetSum(event base.MetricEvent) int64 {
	return n.metric.GetSum(event)
}

// GetMaxAvg 时间窗口内的 最大值*总的bucket数/时间*1000(转成秒)
func (n *BaseStatNode) GetMaxAvg(event base.MetricEvent) float64 {
	return float64(n.metric.GetMaxOfSingleBucket(event)) * float64(n.sampleCount) / float64(n.intervalMs) * 1000.0
}

func (n *BaseStatNode) AddCount(event base.MetricEvent, count int64) {
	n.arr.AddCount(event, count)
}

func (n *BaseStatNode) UpdateConcurrency(concurrency int32) {
	n.arr.UpdateConcurrency(concurrency)
}

// AvgRT 平均响应时间 总请求数/总的rt
func (n *BaseStatNode) AvgRT() float64 {
	complete := n.metric.GetSum(base.MetricEventComplete)
	if complete <= 0 {
		return float64(0.0)
	}
	return float64(n.metric.GetSum(base.MetricEventRt) / complete)
}

// MinRT 获取当前时间最低的rt
func (n *BaseStatNode) MinRT() float64 {
	return float64(n.metric.MinRT())
}

func (n *BaseStatNode) MaxConcurrency() int32 {
	return n.metric.MaxConcurrency()
}

// CurrentConcurrency 获取并发请求数
func (n *BaseStatNode) CurrentConcurrency() int32 {
	return atomic.LoadInt32(&(n.concurrency))
}

// IncreaseConcurrency 增加获取到资源的并发请求数   获取到资源加+1
func (n *BaseStatNode) IncreaseConcurrency() {
	n.UpdateConcurrency(atomic.AddInt32(&(n.concurrency), 1))
}

// DecreaseConcurrency 资源用完减-1
func (n *BaseStatNode) DecreaseConcurrency() {
	atomic.AddInt32(&(n.concurrency), -1)
}

// GenerateReadStat 重用arr 重新生成NewSlidingWindowMetric
func (n *BaseStatNode) GenerateReadStat(sampleCount uint32, intervalInMs uint32) (base.ReadStat, error) {
	return sbase.NewSlidingWindowMetric(sampleCount, intervalInMs, n.arr)
}

// DefaultMetric 返回metric 重用NewSlidingWindowMetric
func (n *BaseStatNode) DefaultMetric() base.ReadStat {
	return n.metric
}

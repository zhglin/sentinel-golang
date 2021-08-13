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

package hotspot

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// ControlBehavior indicates the traffic shaping behaviour.
// 表示热点参数流量控制器的控制行为类型
type ControlBehavior int32

const (
	// Reject 表示如果当前统计周期(DurationInSec)内，统计结构内参数的token已经用完了，就直接拒绝，如果没用完就获取token，通过检查。
	Reject ControlBehavior = iota
	// Throttling 表示匀速排队的统计策略。
	Throttling
)

func (t ControlBehavior) String() string {
	switch t {
	case Reject:
		return "Reject"
	case Throttling:
		return "Throttling"
	default:
		return strconv.Itoa(int(t))
	}
}

// MetricType represents the target metric type.
// 表示规则的流控指标类型。
type MetricType int32

const (
	// Concurrency represents concurrency count.
	// 表示并发数。
	Concurrency MetricType = iota
	// QPS represents request count per second.
	// 表示每秒请求计数。
	QPS
)

func (t MetricType) String() string {
	switch t {
	case Concurrency:
		return "Concurrency"
	case QPS:
		return "QPS"
	default:
		return "Undefined"
	}
}

// Rule represents the hotspot(frequent) parameter flow control rule
type Rule struct {
	// ID is the unique id
	ID string `json:"id,omitempty"`
	// Resource is the resource name
	// 资源名
	Resource string `json:"resource"`
	// MetricType indicates the metric type for checking logic.
	// For Concurrency metric, hotspot module will check the each hot parameter's concurrency,
	//		if concurrency exceeds the Threshold, reject the traffic directly.
	// For QPS metric, hotspot module will check the each hot parameter's QPS,
	//		the ControlBehavior decides the behavior of traffic shaping controller
	// MetricType检查逻辑的度量类型。
	//对于并发度量，hotspot模块将检查每个热参数的并发性，如果并发数超过阈值，直接拒绝流量。
	//对于QPS度量，热点模块将检查每个热参数的QPS，控制行为决定流量整形控制器的行为
	MetricType MetricType `json:"metricType"`
	// ControlBehavior indicates the traffic shaping behaviour.
	// ControlBehavior only takes effect when MetricType is QPS
	// 表示热点参数流量控制器的控制行为，
	// Sentinel支持两种控制行为：Reject(拒绝)和Throttling(匀速排队)，需要强调的是，ControlBehavior仅仅在MetricType是QPS时候才生效。
	ControlBehavior ControlBehavior `json:"controlBehavior"`
	// ParamIndex is the index in context arguments slice.
	// if ParamIndex is great than or equals to zero, ParamIndex means the <ParamIndex>-th parameter
	// if ParamIndex is the negative, ParamIndex means the reversed <ParamIndex>-th parameter
	// ParamIndex是上下文参数切片中的索引。
	// 如果ParamIndex大于或等于0，则ParamIndex表示<ParamIndex>-th参数
	// 如果ParamIndex为负数，则ParamIndex表示反向的<ParamIndex>-th参数
	// api.WithArgs()
	ParamIndex int `json:"paramIndex"`
	// ParamKey is the key in EntryContext.Input.Attachments map.
	// ParamKey can be used as a supplement to ParamIndex to facilitate rules to quickly obtain parameter from a large number of parameters
	// ParamKey is mutually exclusive with ParamIndex, ParamKey has the higher priority than ParamIndex
	// ParamKey是EntryContext.Input.Attachments映射中的键。
	// ParamKey可以作为ParamIndex的补充，方便规则从大量参数中快速获取参数
	// ParamKey和ParamIndex互斥，ParamKey优先级高于ParamIndex
	ParamKey string `json:"paramKey"`
	// Threshold is the threshold to trigger rejection
	// Threshold是触发拒绝的阈值（针对每个热点参数）
	Threshold int64 `json:"threshold"`
	// MaxQueueingTimeMs only takes effect when ControlBehavior is Throttling and MetricType is QPS
	// MaxQueueingTimeMs只在ControlBehavior为Throttling且MetricType为QPS时生效
	// 最大排队等待时长（仅在匀速排队模式 + QPS 下生效）
	MaxQueueingTimeMs int64 `json:"maxQueueingTimeMs"`
	// BurstCount is the silent count
	// BurstCount only takes effect when ControlBehavior is Reject and MetricType is QPS
	// BurstCount是静默计数
	// 只有当ControlBehavior为Reject, MetricType为QPS时，BurstCount才会生效
	BurstCount int64 `json:"burstCount"`
	// DurationInSec is the time interval in statistic
	// DurationInSec only takes effect when MetricType is QPS
	// DurationInSec是统计的时间间隔
	// 当MetricType为QPS时，DurationInSec才生效
	DurationInSec int64 `json:"durationInSec"`
	// ParamsMaxCapacity is the max capacity of cache statistic
	// ParamsMaxCapacity是缓存统计的最大容量
	ParamsMaxCapacity int64 `json:"paramsMaxCapacity"`
	// SpecificItems indicates the special threshold for specific value
	// 特定参数的特殊阈值配置，可以针对指定的参数值单独设置限流阈值，不受前面 Threshold 阈值的限制。
	SpecificItems map[interface{}]int64 `json:"specificItems"`
}

func (r *Rule) String() string {
	b, err := json.Marshal(r)
	if err != nil {
		// Return the fallback string
		return fmt.Sprintf("{Id:%s, Resource:%s, MetricType:%+v, ControlBehavior:%+v, ParamIndex:%d, ParamKey:%s, Threshold:%d, MaxQueueingTimeMs:%d, BurstCount:%d, DurationInSec:%d, ParamsMaxCapacity:%d, SpecificItems:%+v}",
			r.ID, r.Resource, r.MetricType, r.ControlBehavior, r.ParamIndex, r.ParamKey, r.Threshold, r.MaxQueueingTimeMs, r.BurstCount, r.DurationInSec, r.ParamsMaxCapacity, r.SpecificItems)
	}
	return string(b)
}
func (r *Rule) ResourceName() string {
	return r.Resource
}

// IsStatReusable checks whether current rule is "statistically" equal to the given rule.
// 是否能重用
func (r *Rule) IsStatReusable(newRule *Rule) bool {
	return r.Resource == newRule.Resource && r.ControlBehavior == newRule.ControlBehavior && r.ParamsMaxCapacity == newRule.ParamsMaxCapacity && r.DurationInSec == newRule.DurationInSec && r.MetricType == newRule.MetricType
}

// Equals checks whether current rule is consistent with the given rule.
// 是否相等
func (r *Rule) Equals(newRule *Rule) bool {
	baseCheck := r.Resource == newRule.Resource && r.MetricType == newRule.MetricType && r.ControlBehavior == newRule.ControlBehavior && r.ParamsMaxCapacity == newRule.ParamsMaxCapacity && r.ParamIndex == newRule.ParamIndex && r.ParamKey == newRule.ParamKey && r.Threshold == newRule.Threshold && r.DurationInSec == newRule.DurationInSec && reflect.DeepEqual(r.SpecificItems, newRule.SpecificItems)
	if !baseCheck {
		return false
	}
	if r.ControlBehavior == Reject {
		return r.BurstCount == newRule.BurstCount
	} else if r.ControlBehavior == Throttling {
		return r.MaxQueueingTimeMs == newRule.MaxQueueingTimeMs
	} else {
		return false
	}
}

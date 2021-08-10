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

package flow

import (
	"encoding/json"
	"fmt"

	"github.com/alibaba/sentinel-golang/util"
)

// RelationStrategy indicates the flow control strategy based on the relation of invocations.
type RelationStrategy int32

const (
	// CurrentResource means flow control by current resource directly.
	CurrentResource RelationStrategy = iota
	// AssociatedResource means flow control by the associated resource rather than current resource.
	AssociatedResource
)

func (s RelationStrategy) String() string {
	switch s {
	case CurrentResource:
		return "CurrentResource"
	case AssociatedResource:
		return "AssociatedResource"
	default:
		return "Undefined"
	}
}

type TokenCalculateStrategy int32

const (
	Direct TokenCalculateStrategy = iota //表示直接使用字段Threshold作为阈值
	WarmUp                               //使用预热方式计算Token的阈值。
	MemoryAdaptive
)

func (s TokenCalculateStrategy) String() string {
	switch s {
	case Direct:
		return "Direct"
	case WarmUp:
		return "WarmUp"
	case MemoryAdaptive:
		return "MemoryAdaptive"
	default:
		return "Undefined"
	}
}

// ControlBehavior defines the behavior when requests have reached the capacity of the resource.
type ControlBehavior int32

const (
	Reject ControlBehavior = iota // 表示超过阈值直接拒绝
	// Throttling indicates that pending requests will be throttled, wait in queue (until free capacity is available)
	Throttling // 表示匀速排队
)

func (s ControlBehavior) String() string {
	switch s {
	case Reject:
		return "Reject"
	case Throttling:
		return "Throttling"
	default:
		return "Undefined"
	}
}

// Rule describes the strategy of flow control, the flow control strategy is based on QPS statistic metric
type Rule struct {
	// ID represents the unique ID of the rule (optional).
	ID string `json:"id,omitempty"`
	// Resource represents the resource name.
	// 资源名，即规则的作用目标。
	Resource string `json:"resource"`
	// 当前流量控制器的Token计算策略
	TokenCalculateStrategy TokenCalculateStrategy `json:"tokenCalculateStrategy"`
	// 表示流量控制器的控制策略
	ControlBehavior ControlBehavior `json:"controlBehavior"`
	// Threshold means the threshold during StatIntervalInMs
	// If StatIntervalInMs is 1000(1 second), Threshold means QPS
	// 表示流控阈值；如果字段 StatIntervalInMs 是1000(也就是1秒)，那么Threshold就表示QPS，流量控制器也就会依据资源的QPS来做流控。
	Threshold float64 `json:"threshold"`
	// 调用关系限流策略，CurrentResource表示使用当前规则的resource做流控；AssociatedResource表示使用关联的resource做流控，关联的resource在字段RefResource 定义
	RelationStrategy RelationStrategy `json:"relationStrategy"`
	// 关联的resource
	RefResource string `json:"refResource"`
	// MaxQueueingTimeMs only takes effect when ControlBehavior is Throttling.
	// When MaxQueueingTimeMs is 0, it means Throttling only controls interval of requests,
	// and requests exceeding the threshold will be rejected directly.
	// 匀速排队的最大等待时间，该字段仅仅对Throttling ControlBehavior生效；
	// 当MaxQueueingTimeMs为0时，表示Throttling仅控制请求的间隔时间，超过该阈值的请求将被直接拒绝。
	MaxQueueingTimeMs uint32 `json:"maxQueueingTimeMs"`
	// 代表期待系统进入稳定状态的时间(即预热时长)
	WarmUpPeriodSec uint32 `json:"warmUpPeriodSec"`
	// 预热的因子，默认是3，该值的设置会影响预热的速度
	WarmUpColdFactor uint32 `json:"warmUpColdFactor"`
	// StatIntervalInMs indicates the statistic interval and it's the optional setting for flow Rule.
	// If user doesn't set StatIntervalInMs, that means using default metric statistic of resource.
	// If the StatIntervalInMs user specifies can not reuse the global statistic of resource,
	// 		sentinel will generate independent statistic structure for this rule.
	// StatIntervalInMs表示统计间隔，它是流规则的可选设置。
	// 如果用户没有设置StatIntervalInMs，这意味着使用资源的默认度量统计。
	// 如果StatIntervalInMs用户指定不能重用全局资源统计，哨兵将为该规则生成独立的统计结构。
	StatIntervalInMs uint32 `json:"statIntervalInMs"`

	// adaptive flow control algorithm related parameters
	// limitation: LowMemUsageThreshold > HighMemUsageThreshold && MemHighWaterMarkBytes > MemLowWaterMarkBytes
	// if the current memory usage is less than or equals to MemLowWaterMarkBytes, threshold == LowMemUsageThreshold
	// if the current memory usage is more than or equals to MemHighWaterMarkBytes, threshold == HighMemUsageThreshold
	// if  the current memory usage is in (MemLowWaterMarkBytes, MemHighWaterMarkBytes), threshold is in (HighMemUsageThreshold, LowMemUsageThreshold)
	LowMemUsageThreshold  int64 `json:"lowMemUsageThreshold"`
	HighMemUsageThreshold int64 `json:"highMemUsageThreshold"`
	MemLowWaterMarkBytes  int64 `json:"memLowWaterMarkBytes"`
	MemHighWaterMarkBytes int64 `json:"memHighWaterMarkBytes"`
}

// 两个rule是否相等
func (r *Rule) isEqualsTo(newRule *Rule) bool {
	if newRule == nil {
		return false
	}
	if !(r.Resource == newRule.Resource && r.RelationStrategy == newRule.RelationStrategy &&
		r.RefResource == newRule.RefResource && r.StatIntervalInMs == newRule.StatIntervalInMs &&
		r.TokenCalculateStrategy == newRule.TokenCalculateStrategy && r.ControlBehavior == newRule.ControlBehavior &&
		util.Float64Equals(r.Threshold, newRule.Threshold) &&
		r.MaxQueueingTimeMs == newRule.MaxQueueingTimeMs && r.WarmUpPeriodSec == newRule.WarmUpPeriodSec &&
		r.WarmUpColdFactor == newRule.WarmUpColdFactor &&
		r.LowMemUsageThreshold == newRule.LowMemUsageThreshold && r.HighMemUsageThreshold == newRule.HighMemUsageThreshold &&
		r.MemLowWaterMarkBytes == newRule.MemLowWaterMarkBytes && r.MemHighWaterMarkBytes == newRule.MemHighWaterMarkBytes) {

		return false
	}
	return true
}

// 是否可重用的
func (r *Rule) isStatReusable(newRule *Rule) bool {
	if newRule == nil {
		return false
	}
	return r.Resource == newRule.Resource && r.RelationStrategy == newRule.RelationStrategy &&
		r.RefResource == newRule.RefResource && r.StatIntervalInMs == newRule.StatIntervalInMs &&
		r.needStatistic() && newRule.needStatistic()
}

// 是否需要单独进行统计计数
func (r *Rule) needStatistic() bool {
	return r.TokenCalculateStrategy == WarmUp || r.ControlBehavior == Reject
}

func (r *Rule) String() string {
	b, err := json.Marshal(r)
	if err != nil {
		// Return the fallback string
		return fmt.Sprintf("Rule{Resource=%s, TokenCalculateStrategy=%s, ControlBehavior=%s, "+
			"Threshold=%.2f, RelationStrategy=%s, RefResource=%s, MaxQueueingTimeMs=%d, WarmUpPeriodSec=%d, WarmUpColdFactor=%d, StatIntervalInMs=%d, "+
			"LowMemUsageThreshold=%v, HighMemUsageThreshold=%v, MemLowWaterMarkBytes=%v, MemHighWaterMarkBytes=%v}",
			r.Resource, r.TokenCalculateStrategy, r.ControlBehavior, r.Threshold, r.RelationStrategy, r.RefResource,
			r.MaxQueueingTimeMs, r.WarmUpPeriodSec, r.WarmUpColdFactor, r.StatIntervalInMs,
			r.LowMemUsageThreshold, r.HighMemUsageThreshold, r.MemLowWaterMarkBytes, r.MemHighWaterMarkBytes)
	}
	return string(b)
}

func (r *Rule) ResourceName() string {
	return r.Resource
}

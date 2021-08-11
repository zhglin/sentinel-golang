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

package circuitbreaker

import (
	"fmt"

	"github.com/alibaba/sentinel-golang/util"
)

// Strategy represents the strategy of circuit breaker.
// Each strategy is associated with one rule type.
type Strategy uint32

const (
	// SlowRequestRatio strategy changes the circuit breaker state based on slow request ratio
	// Sentinel的熔断器不在静默期，并且慢调用的比例大于设置的阈值，则接下来的熔断周期内对资源的访问会自动地被熔断。该策略下需要设置允许的调用RT临界值(即最大的响应时间)，对该资源访问的响应时间大于该阈值则统计为慢调用。
	SlowRequestRatio Strategy = iota
	// ErrorRatio strategy changes the circuit breaker state based on error request ratio
	// Sentinel的熔断器不在静默期，并且在统计周期内资源请求访问异常的比例大于设定的阈值，则接下来的熔断周期内对资源的访问会自动地被熔断。
	ErrorRatio
	// ErrorCount strategy changes the circuit breaker state based on error amount
	// Sentinel的熔断器不在静默期，并且在统计周期内资源请求访问异常数大于设定的阈值，则接下来的熔断周期内对资源的访问会自动地被熔断。
	ErrorCount
)

func (s Strategy) String() string {
	switch s {
	case SlowRequestRatio:
		return "SlowRequestRatio"
	case ErrorRatio:
		return "ErrorRatio"
	case ErrorCount:
		return "ErrorCount"
	default:
		return "Undefined"
	}
}

// Rule encompasses the fields of circuit breaking rule.
// 熔断规则
type Rule struct {
	// unique id 唯一id
	Id string `json:"id,omitempty"`
	// resource name 资源名称
	Resource string `json:"resource"`
	// 熔断策略 SlowRequestRatio,ErrorRatio,ErrorCount
	Strategy Strategy `json:"strategy"`
	// RetryTimeoutMs represents recovery timeout (in milliseconds) before the circuit breaker opens.
	// During the open period, no requests are permitted until the timeout has elapsed.
	// After that, the circuit breaker will transform to half-open state for trying a few "trial" requests.
	// 表示断路器断开前的恢复超时(以毫秒为单位)。
	// s在打开期间，在超时结束之前，不允许任何请求。之后，断路器将转变为半开状态，以尝试几个“试验”请求。
	RetryTimeoutMs uint32 `json:"retryTimeoutMs"`
	// MinRequestAmount represents the minimum number of requests (in an active statistic time span)
	// that can trigger circuit breaking.
	// MinRequestAmount表示可以触发断路的最小请求数(在一个活动统计时间范围内)。
	// 静默期是指一个最小的静默请求数，在一个统计周期内，如果对资源的请求数小于设置的静默数，那么熔断器将不会基于其统计值去更改熔断器的状态。
	MinRequestAmount uint64 `json:"minRequestAmount"`
	// StatIntervalMs represents statistic time interval of the internal circuit breaker (in ms).
	// Currently the statistic interval is collected by sliding window.
	// StatIntervalMs内部断路器的统计时间间隔，单位为ms。目前统计时间间隔是通过滑动窗口收集的。
	StatIntervalMs uint32 `json:"statIntervalMs"`
	// StatSlidingWindowBucketCount represents the bucket count of statistic sliding window.
	// The statistic will be more precise as the bucket count increases, but the memory cost increases too.
	// The following must be true — “StatIntervalMs % StatSlidingWindowBucketCount == 0”,
	// otherwise StatSlidingWindowBucketCount will be replaced by 1.
	// If it is not set, default value 1 will be used.
	// StatSlidingWindowBucketCount统计滑动窗口的桶数。随着桶数的增加，统计数据会更加精确，但内存成本也会增加。
	// 下面必须为真- " StatIntervalMs % StatSlidingWindowBucketCount == 0 "，否则StatSlidingWindowBucketCount将被1替换。如果未设置，则使用默认值1。
	StatSlidingWindowBucketCount uint32 `json:"statSlidingWindowBucketCount"`
	// MaxAllowedRtMs indicates that any invocation whose response time exceeds this value (in ms)
	// will be recorded as a slow request.
	// MaxAllowedRtMs only takes effect for SlowRequestRatio strategy
	// MaxAllowedRtMs表示响应时间超过该值的任何调用(以ms为单位) 将被记录为一个缓慢的请求。
	// maxallowedrtm只对SlowRequestRatio策略生效
	MaxAllowedRtMs uint64 `json:"maxAllowedRtMs"`
	// Threshold represents the threshold of circuit breaker.
	// for SlowRequestRatio, it represents the max slow request ratio
	// for ErrorRatio, it represents the max error request ratio
	// for ErrorCount, it represents the max error request count
	// Threshold表示断路器的阈值。
	// 对于SlowRequestRatio，它表示最大的慢请求比率
	// 对于ErrorRatio，它表示最大的错误请求比率
	// 对于ErrorCount，它表示最大错误请求计数
	Threshold float64 `json:"threshold"`
}

func (r *Rule) String() string {
	// fallback string
	return fmt.Sprintf("{id=%s, resource=%s, strategy=%s, RetryTimeoutMs=%d, MinRequestAmount=%d, StatIntervalMs=%d, StatSlidingWindowBucketCount=%d, MaxAllowedRtMs=%d, Threshold=%f}",
		r.Id, r.Resource, r.Strategy, r.RetryTimeoutMs, r.MinRequestAmount, r.StatIntervalMs, r.StatSlidingWindowBucketCount, r.MaxAllowedRtMs, r.Threshold)
}

func (r *Rule) isStatReusable(newRule *Rule) bool {
	if newRule == nil {
		return false
	}
	return r.Resource == newRule.Resource && r.Strategy == newRule.Strategy && r.StatIntervalMs == newRule.StatIntervalMs &&
		r.StatSlidingWindowBucketCount == newRule.StatSlidingWindowBucketCount
}

func (r *Rule) ResourceName() string {
	return r.Resource
}

// 两个规则基本信息是否相同
func (r *Rule) isEqualsToBase(newRule *Rule) bool {
	if newRule == nil {
		return false
	}
	return r.Resource == newRule.Resource && r.Strategy == newRule.Strategy && r.RetryTimeoutMs == newRule.RetryTimeoutMs &&
		r.MinRequestAmount == newRule.MinRequestAmount && r.StatIntervalMs == newRule.StatIntervalMs && r.StatSlidingWindowBucketCount == newRule.StatSlidingWindowBucketCount
}

// 两个规则是否阀值相同
func (r *Rule) isEqualsTo(newRule *Rule) bool {
	if !r.isEqualsToBase(newRule) {
		return false
	}

	switch newRule.Strategy {
	case SlowRequestRatio:
		return r.MaxAllowedRtMs == newRule.MaxAllowedRtMs && util.Float64Equals(r.Threshold, newRule.Threshold)
	case ErrorRatio:
		return util.Float64Equals(r.Threshold, newRule.Threshold)
	case ErrorCount:
		return util.Float64Equals(r.Threshold, newRule.Threshold)
	default:
		return false
	}
}

// 获取bucket数量
func getRuleStatSlidingWindowBucketCount(r *Rule) uint32 {
	interval := r.StatIntervalMs
	bucketCount := r.StatSlidingWindowBucketCount
	if bucketCount == 0 || interval%bucketCount != 0 {
		bucketCount = 1
	}
	return bucketCount
}

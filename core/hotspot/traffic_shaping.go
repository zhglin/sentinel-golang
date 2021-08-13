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
	"fmt"
	"math"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/hotspot/cache"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

// TrafficShapingController 流量控制器接口
type TrafficShapingController interface {
	// PerformChecking 校验是否超过阀值
	PerformChecking(arg interface{}, batchCount int64) *base.TokenResult

	// BoundParamIndex ctx.Input.Args中的下标
	BoundParamIndex() int

	// ExtractArgs 提取参数值
	ExtractArgs(ctx *base.EntryContext) interface{}

	// BoundMetric 获取统计信息
	BoundMetric() *ParamsMetric

	// BoundRule 获取对应的规则
	BoundRule() *Rule
}

type baseTrafficShapingController struct {
	r *Rule

	res           string                // 资源名称
	metricType    MetricType            // 流控指标类型
	paramIndex    int                   // ctx.Input.Args中的下标
	paramKey      string                // ctx.Input.Attachments中的键
	threshold     int64                 //阀值
	specificItems map[interface{}]int64 // 特定参数的特殊阈值配置
	durationInSec int64                 // 统计时间间隔

	metric *ParamsMetric // 统计信息
}

// 根据统计信息rule创建控制器
func newBaseTrafficShapingControllerWithMetric(r *Rule, metric *ParamsMetric) *baseTrafficShapingController {
	if r.SpecificItems == nil {
		r.SpecificItems = make(map[interface{}]int64)
	}
	return &baseTrafficShapingController{
		r:             r,
		res:           r.Resource,
		metricType:    r.MetricType,
		paramIndex:    r.ParamIndex,
		paramKey:      r.ParamKey,
		threshold:     r.Threshold,
		specificItems: r.SpecificItems,
		durationInSec: r.DurationInSec,
		metric:        metric,
	}
}

// 创建rule对应的统计信息
func newBaseTrafficShapingController(r *Rule) *baseTrafficShapingController {
	switch r.MetricType {
	case QPS:
		size := 0
		if r.ParamsMaxCapacity > 0 {
			size = int(r.ParamsMaxCapacity)
		} else if r.DurationInSec == 0 {
			size = ParamsMaxCapacity
		} else {
			size = int(math.Min(float64(ParamsMaxCapacity), float64(ParamsCapacityBase*r.DurationInSec)))
		}
		if size <= 0 {
			logging.Warn("[HotSpot newBaseTrafficShapingController] Invalid size of cache, so use default value for ParamsMaxCapacity and ParamsCapacityBase",
				"ParamsMaxCapacity", ParamsMaxCapacity, "ParamsCapacityBase", ParamsCapacityBase)
			size = ParamsMaxCapacity
		}
		metric := &ParamsMetric{
			RuleTimeCounter:  cache.NewLRUCacheMap(size),
			RuleTokenCounter: cache.NewLRUCacheMap(size),
		}
		return newBaseTrafficShapingControllerWithMetric(r, metric)
	case Concurrency:
		size := 0
		if r.ParamsMaxCapacity > 0 {
			size = int(r.ParamsMaxCapacity)
		} else {
			size = ConcurrencyMaxCount
		}
		metric := &ParamsMetric{
			ConcurrencyCounter: cache.NewLRUCacheMap(size),
		}
		return newBaseTrafficShapingControllerWithMetric(r, metric)
	default:
		logging.Error(errors.New("unsupported metric type"), "Ignoring the rule due to unsupported  metric type in Rule.newBaseTrafficShapingController()", "MetricType", r.MetricType.String())
		return nil
	}
}

// BoundMetric 返回统计信息
func (c *baseTrafficShapingController) BoundMetric() *ParamsMetric {
	return c.metric
}

// 并发数的阀值校验
func (c *baseTrafficShapingController) performCheckingForConcurrencyMetric(arg interface{}) *base.TokenResult {
	specificItem := c.specificItems

	// 写入个0，看看是否存在
	initConcurrency := new(int64)
	*initConcurrency = 0
	concurrencyPtr := c.metric.ConcurrencyCounter.AddIfAbsent(arg, initConcurrency)
	if concurrencyPtr == nil {
		// First to access this arg
		// 第一次访问这个参数
		return nil
	}

	// 之前已经有，获取之前的数量
	concurrency := atomic.LoadInt64(concurrencyPtr)
	// 最新并发数量
	concurrency++
	// 特定参数的阀值
	if specificConcurrency, existed := specificItem[arg]; existed {
		if concurrency <= specificConcurrency {
			return nil
		}
		msg := fmt.Sprintf("hotspot specific concurrency check blocked, arg: %v", arg)
		return base.NewTokenResultBlockedWithCause(base.BlockTypeHotSpotParamFlow, msg, c.BoundRule(), concurrency)
	}

	// 非特定参数的阀值
	threshold := c.threshold
	if concurrency <= threshold {
		return nil
	}
	msg := fmt.Sprintf("hotspot concurrency check blocked, arg: %v", arg)
	return base.NewTokenResultBlockedWithCause(base.BlockTypeHotSpotParamFlow, msg, c.BoundRule(), concurrency)
}

// rejectTrafficShapingController use Reject strategy
// 直接拒绝的策略
type rejectTrafficShapingController struct {
	baseTrafficShapingController
	burstCount int64 // 静默
}

// rejectTrafficShapingController use Throttling strategy
// 匀速排队的统计策略
type throttlingTrafficShapingController struct {
	baseTrafficShapingController
	maxQueueingTimeMs int64 // 最大排队等待时长
}

func (c *baseTrafficShapingController) BoundRule() *Rule {
	return c.r
}

func (c *baseTrafficShapingController) BoundParamIndex() int {
	return c.paramIndex
}

// ExtractArgs matches the arg from ctx based on TrafficShapingController
// return nil if match failed.
// 根据TrafficShapingController从ctx匹配arg如果匹配失败返回nil。
func (c *baseTrafficShapingController) ExtractArgs(ctx *base.EntryContext) (value interface{}) {
	if c == nil {
		return nil
	}
	value = c.extractAttachmentArgs(ctx)
	if value != nil {
		return
	}
	value = c.extractArgs(ctx)
	if value != nil {
		return
	}
	return
}

// 根据c.paramIndex从ctx.Input.Args获取数据值
func (c *baseTrafficShapingController) extractArgs(ctx *base.EntryContext) interface{} {
	args := ctx.Input.Args
	idx := c.BoundParamIndex()
	if idx < 0 {
		idx = len(args) + idx
	}
	if idx < 0 {
		if logging.DebugEnabled() {
			logging.Debug("[extractArgs] The param index of hotspot traffic shaping controller is invalid",
				"args", args, "paramIndex", c.BoundParamIndex())
		}
		return nil
	}
	if idx >= len(args) {
		if logging.DebugEnabled() {
			logging.Debug("[extractArgs] The argument in index doesn't exist",
				"args", args, "paramIndex", c.BoundParamIndex())
		}
		return nil
	}
	return args[idx]
}

// 根据c.paramKey获取ctx.Input.Attachments中的值
func (c *baseTrafficShapingController) extractAttachmentArgs(ctx *base.EntryContext) interface{} {
	attachments := ctx.Input.Attachments

	if attachments == nil {
		if logging.DebugEnabled() {
			logging.Debug("[paramKey] The attachments of ctx is nil",
				"args", attachments, "paramKey", c.paramKey)
		}
		return nil
	}
	if c.paramKey == "" {
		if logging.DebugEnabled() {
			logging.Debug("[paramKey] The param key is nil",
				"args", attachments, "paramKey", c.paramKey)
		}
		return nil
	}
	arg, ok := attachments[c.paramKey]
	if !ok {
		if logging.DebugEnabled() {
			logging.Debug("[paramKey] extracted data does not exist",
				"args", attachments, "paramKey", c.paramKey)
		}
	}

	return arg
}

// PerformChecking 拒绝策略的阀值校验
func (c *rejectTrafficShapingController) PerformChecking(arg interface{}, batchCount int64) *base.TokenResult {
	metric := c.metric
	if metric == nil {
		return nil
	}

	if c.metricType == Concurrency {
		return c.performCheckingForConcurrencyMetric(arg)
	} else if c.metricType > QPS {
		return nil
	}

	// QPS
	timeCounter := metric.RuleTimeCounter
	tokenCounter := metric.RuleTokenCounter
	if timeCounter == nil || tokenCounter == nil {
		return nil
	}

	// calculate available token
	// 计算可用的令牌
	tokenCount := c.threshold
	val, existed := c.specificItems[arg]
	if existed {
		tokenCount = val
	}
	if tokenCount <= 0 {
		msg := fmt.Sprintf("hotspot reject check blocked, threshold is <= 0, arg: %v", arg)
		return base.NewTokenResultBlockedWithCause(base.BlockTypeHotSpotParamFlow, msg, c.BoundRule(), nil)
	}

	// 一次申请的token大于总的
	maxCount := tokenCount + c.burstCount
	if batchCount > maxCount {
		// return blocked because the batch number is more than max count of rejectTrafficShapingController
		msg := fmt.Sprintf("hotspot reject check blocked, request batch count is more than max token count, arg: %v", arg)
		return base.NewTokenResultBlockedWithCause(base.BlockTypeHotSpotParamFlow, msg, c.BoundRule(), nil)
	}

	for {
		// 当前时间
		currentTimeInMs := int64(util.CurrentTimeMillis())
		// 当前参数之前是否添加过令牌
		lastAddTokenTimePtr := timeCounter.AddIfAbsent(arg, &currentTimeInMs)
		if lastAddTokenTimePtr == nil {
			// First to fill token, and consume token immediately
			// 首先要填满令牌，然后立即消费令牌
			leftCount := maxCount - batchCount
			// 记录令牌
			tokenCounter.AddIfAbsent(arg, &leftCount)
			return nil
		}

		// Calculate the time duration since last token was added.
		// 计算自上次添加令牌以来的持续时间。
		passTime := currentTimeInMs - atomic.LoadInt64(lastAddTokenTimePtr)
		// 超过间隔时间，重新填充令牌。
		if passTime > c.durationInSec*1000 {
			// Refill the tokens because statistic window has passed.
			leftCount := maxCount - batchCount
			oldQpsPtr := tokenCounter.AddIfAbsent(arg, &leftCount)
			if oldQpsPtr == nil { // 之前的令牌数已经不存在了
				// Might not be accurate here.
				// 更新lru中的时间，这里可能不准确。
				atomic.StoreInt64(lastAddTokenTimePtr, currentTimeInMs)
				return nil
			} else {
				// refill token 重置令牌
				restQps := atomic.LoadInt64(oldQpsPtr) // 过期前剩余的令牌数
				// 过期的这段时间所能产生的令牌数
				toAddTokenNum := passTime * tokenCount / (c.durationInSec * 1000)
				newQps := int64(0)
				// 当前产生的令牌+剩余的令牌 = 当前时间最新的令牌数
				if toAddTokenNum+restQps > maxCount {
					newQps = maxCount - batchCount
				} else {
					newQps = toAddTokenNum + restQps - batchCount
				}
				if newQps < 0 {
					msg := fmt.Sprintf("hotspot reject check blocked, request batch count is more than available token count, arg: %v", arg)
					return base.NewTokenResultBlockedWithCause(base.BlockTypeHotSpotParamFlow, msg, c.BoundRule(), nil)
				}
				// 原子交换
				if atomic.CompareAndSwapInt64(oldQpsPtr, restQps, newQps) {
					atomic.StoreInt64(lastAddTokenTimePtr, currentTimeInMs)
					return nil
				}
				// 交换失败 重试
				runtime.Gosched()
			}
		} else {
			//check whether the rest of token is enough to batch
			// 检查令牌的剩余部分是否足够批处理
			oldQpsPtr, found := tokenCounter.Get(arg)
			if found {
				oldRestToken := atomic.LoadInt64(oldQpsPtr)
				if oldRestToken-batchCount >= 0 {
					//update
					if atomic.CompareAndSwapInt64(oldQpsPtr, oldRestToken, oldRestToken-batchCount) {
						return nil
					}
				} else {
					msg := fmt.Sprintf("hotspot reject check blocked, request batch count is more than available token count, arg: %v", arg)
					return base.NewTokenResultBlockedWithCause(base.BlockTypeHotSpotParamFlow, msg, c.BoundRule(), nil)
				}
			}
			// 数据刚好过期 重试
			runtime.Gosched()
		}
	}
}

// PerformChecking 匀速排队的统计策略
func (c *throttlingTrafficShapingController) PerformChecking(arg interface{}, batchCount int64) *base.TokenResult {
	metric := c.metric
	if metric == nil {
		return nil
	}

	if c.metricType == Concurrency {
		return c.performCheckingForConcurrencyMetric(arg)
	} else if c.metricType > QPS {
		return nil
	}

	// 校验统计信息
	timeCounter := metric.RuleTimeCounter
	tokenCounter := metric.RuleTokenCounter
	if timeCounter == nil || tokenCounter == nil {
		return nil
	}

	// calculate available token
	// 计算可用的令牌 阀值
	tokenCount := c.threshold
	val, existed := c.specificItems[arg]
	if existed {
		tokenCount = val
	}
	if tokenCount <= 0 {
		msg := fmt.Sprintf("hotspot throttling check blocked, threshold is <= 0, arg: %v", arg)
		return base.NewTokenResultBlockedWithCause(base.BlockTypeHotSpotParamFlow, msg, c.BoundRule(), nil)
	}

	// batchCount个token需要的时间
	intervalCostTime := int64(math.Round(float64(batchCount * c.durationInSec * 1000 / tokenCount)))
	for {
		// 当前毫秒
		currentTimeInMs := int64(util.CurrentTimeMillis())
		// cache中最后一次发放token时间
		lastPassTimePtr := timeCounter.AddIfAbsent(arg, &currentTimeInMs)
		if lastPassTimePtr == nil {
			// first access arg 第一次访问参数
			return nil
		}
		// load the last pass time 最后的时间
		lastPassTime := atomic.LoadInt64(lastPassTimePtr)
		// calculate the expected pass time 计算预期的通过时间
		expectedTime := lastPassTime + intervalCostTime

		// 通过时间小于当前时间 || 通过事件小于最大排队等待时间
		if expectedTime <= currentTimeInMs || expectedTime-currentTimeInMs < c.maxQueueingTimeMs {
			// 排队 更新到当前时间
			if atomic.CompareAndSwapInt64(lastPassTimePtr, lastPassTime, currentTimeInMs) {
				awaitTime := expectedTime - currentTimeInMs // 排队时长
				if awaitTime > 0 {
					atomic.StoreInt64(lastPassTimePtr, expectedTime) // 在更新到下次的时间
					return base.NewTokenResultShouldWait(time.Duration(awaitTime) * time.Millisecond)
				}
				return nil
			} else {
				runtime.Gosched()
			}
		} else {
			// 直接拒绝
			msg := fmt.Sprintf("hotspot throttling check blocked, wait time exceedes max queueing time, arg: %v", arg)
			return base.NewTokenResultBlockedWithCause(base.BlockTypeHotSpotParamFlow, msg, c.BoundRule(), nil)
		}
	}
}

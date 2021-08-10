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
	"math"
	"sync/atomic"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/config"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
)

// WarmUpTrafficShapingCalculator 表示通过预热的方式计算当前统计周期内的最大Token数量
// 预热的计算方式会根据规则中的字段 WarmUpPeriodSec 和 WarmUpColdFactor 来决定预热的曲线。
type WarmUpTrafficShapingCalculator struct {
	owner             *TrafficShapingController
	threshold         float64 // QPS
	warmUpPeriodInSec uint32  // 预热时长
	coldFactor        uint32  // 冷启动系数
	warningToken      uint64  // 稳定的令牌生产速率下令牌桶中存储的令牌数，超过进行冷启动
	maxToken          uint64  // 令牌桶的最大容量
	slope             float64 // 斜率，每秒放行请求数的增长速率
	storedTokens      int64   // 令牌桶当前存储的令牌数量
	lastFilledTime    uint64  // 上一次生产令牌的时间戳
}

func (c *WarmUpTrafficShapingCalculator) BoundOwner() *TrafficShapingController {
	return c.owner
}

func NewWarmUpTrafficShapingCalculator(owner *TrafficShapingController, rule *Rule) TrafficShapingCalculator {
	if rule.WarmUpColdFactor <= 1 {
		rule.WarmUpColdFactor = config.DefaultWarmUpColdFactor
		logging.Warn("[NewWarmUpTrafficShapingCalculator] No set WarmUpColdFactor,use default warm up cold factor value", "defaultWarmUpColdFactor", config.DefaultWarmUpColdFactor)
	}

	// 计算公式
	// http://learn.lianglianglee.com/%E4%B8%93%E6%A0%8F/%E6%B7%B1%E5%85%A5%E7%90%86%E8%A7%A3%20Sentinel%EF%BC%88%E5%AE%8C%EF%BC%89/12%20%E9%99%90%E6%B5%81%E9%99%8D%E7%BA%A7%E4%B8%8E%E6%B5%81%E9%87%8F%E6%95%88%E6%9E%9C%E6%8E%A7%E5%88%B6%E5%99%A8%EF%BC%88%E4%B8%8B%EF%BC%89.md
	// https://www.javadoop.com/post/rate-limiter
	warningToken := uint64((float64(rule.WarmUpPeriodSec) * rule.Threshold) / float64(rule.WarmUpColdFactor-1))

	maxToken := warningToken + uint64(2*float64(rule.WarmUpPeriodSec)*rule.Threshold/float64(1.0+rule.WarmUpColdFactor))

	// 斜率
	slope := float64(rule.WarmUpColdFactor-1.0) / rule.Threshold / float64(maxToken-warningToken)

	warmUpTrafficShapingCalculator := &WarmUpTrafficShapingCalculator{
		owner:             owner,
		warmUpPeriodInSec: rule.WarmUpPeriodSec,
		coldFactor:        rule.WarmUpColdFactor,
		warningToken:      warningToken,
		maxToken:          maxToken,
		slope:             slope,
		threshold:         rule.Threshold,
		storedTokens:      0,
		lastFilledTime:    0,
	}

	return warmUpTrafficShapingCalculator
}

func (c *WarmUpTrafficShapingCalculator) CalculateAllowedTokens(_ uint32, _ int32) float64 {
	metricReadonlyStat := c.BoundOwner().boundStat.readOnlyMetric
	previousQps := metricReadonlyStat.GetPreviousQPS(base.MetricEventPass) // 当前时间的QPS
	c.syncToken(previousQps)

	restToken := atomic.LoadInt64(&c.storedTokens)
	if restToken < 0 { // 在并发中，currentValue := atomic.AddInt64(&c.storedTokens, int64(-passQps)); 可能导致storedTokens小于0
		restToken = 0
	}
	if restToken >= int64(c.warningToken) {
		aboveToken := restToken - int64(c.warningToken)
		warningQps := math.Nextafter(1.0/(float64(aboveToken)*c.slope+1.0/c.threshold), math.MaxFloat64)
		return warningQps
	} else {
		return c.threshold
	}
}

// 每秒更新storedTokens 令牌桶数量
func (c *WarmUpTrafficShapingCalculator) syncToken(passQps float64) {
	// 当前时间 去掉毫秒，取秒
	currentTime := util.CurrentTimeMillis()
	currentTime = currentTime - currentTime%1000
	// 控制每秒只更新一次
	oldLastFillTime := atomic.LoadUint64(&c.lastFilledTime)
	if currentTime <= oldLastFillTime {
		return
	}

	oldValue := atomic.LoadInt64(&c.storedTokens)
	newValue := c.coolDownTokens(currentTime, passQps) // 这一秒的令牌数

	if atomic.CompareAndSwapInt64(&c.storedTokens, oldValue, newValue) {
		if currentValue := atomic.AddInt64(&c.storedTokens, int64(-passQps)); currentValue < 0 {
			atomic.StoreInt64(&c.storedTokens, 0)
		}
		atomic.StoreUint64(&c.lastFilledTime, currentTime)
	}
}

// 计算currentTime时间的令牌数
func (c *WarmUpTrafficShapingCalculator) coolDownTokens(currentTime uint64, passQps float64) int64 {
	oldValue := atomic.LoadInt64(&c.storedTokens) // 当前的令牌数量
	newValue := oldValue

	// Prerequisites for adding a token:
	// When token consumption is much lower than the warning line
	// 添加令牌的前提条件: 当令牌消耗远低于警戒线时
	if oldValue < int64(c.warningToken) {
		newValue = int64(float64(oldValue) + (float64(currentTime)-float64(atomic.LoadUint64(&c.lastFilledTime)))*c.threshold/1000.0)
	} else if oldValue > int64(c.warningToken) {
		if passQps < float64(uint32(c.threshold)/c.coldFactor) {
			newValue = int64(float64(oldValue) + float64(currentTime-atomic.LoadUint64(&c.lastFilledTime))*c.threshold/1000.0)
		}
	}

	if newValue <= int64(c.maxToken) {
		return newValue
	} else {
		return int64(c.maxToken)
	}
}

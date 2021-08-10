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
	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/metrics"
)

// TrafficShapingCalculator calculates the actual traffic shaping threshold
// based on the threshold of rule and the traffic shaping strategy.
// 根据规则阈值和流量整形策略计算实际流量整形阈值。
type TrafficShapingCalculator interface {
	BoundOwner() *TrafficShapingController
	CalculateAllowedTokens(batchCount uint32, flag int32) float64
}

// TrafficShapingChecker performs checking according to current metrics and the traffic
// shaping strategy, then yield the token result.
// 根据当前指标和流量整形策略进行检查，然后生成令牌结果。
type TrafficShapingChecker interface {
	BoundOwner() *TrafficShapingController
	DoCheck(resStat base.StatNode, batchCount uint32, threshold float64) *base.TokenResult
}

// standaloneStatistic indicates the independent statistic for each TrafficShapingController
// 表示每个TrafficShapingController的独立统计
type standaloneStatistic struct {
	// reuseResourceStat indicates whether current standaloneStatistic reuse the current resource's global statistic
	// 指示当前独立统计是否重用当前资源的全局统计
	reuseResourceStat bool
	// readOnlyMetric is the readonly metric statistic.
	// if reuseResourceStat is true, it would be the reused SlidingWindowMetric
	// if reuseResourceStat is false, it would be the BucketLeapArray
	// readOnlyMetric是只读度量统计。
	// 如果reuseResourceStat为true，它将是重用的SlidingWindowMetric
	// 如果reuseResourceStat为false，它将是BucketLeapArray
	readOnlyMetric base.ReadStat
	// writeOnlyMetric is the write only metric statistic.
	// if reuseResourceStat is true, it would be nil
	// if reuseResourceStat is false, it would be the BucketLeapArray
	// writeOnlyMetric是只写度量统计。
	// 如果reuseResourceStat为true，它将为nil
	// 如果reuseResourceStat为false，它将是BucketLeapArray
	writeOnlyMetric base.WriteStat
}

// TrafficShapingController 流量控制器
type TrafficShapingController struct {
	flowCalculator TrafficShapingCalculator
	flowChecker    TrafficShapingChecker

	rule *Rule
	// boundStat is the statistic of current TrafficShapingController
	boundStat standaloneStatistic
}

// NewTrafficShapingController 使用指定的rule standaloneStatistic创建TrafficShapingController
func NewTrafficShapingController(rule *Rule, boundStat *standaloneStatistic) (*TrafficShapingController, error) {
	return &TrafficShapingController{rule: rule, boundStat: *boundStat}, nil
}

func (t *TrafficShapingController) BoundRule() *Rule {
	return t.rule
}

func (t *TrafficShapingController) FlowChecker() TrafficShapingChecker {
	return t.flowChecker
}

func (t *TrafficShapingController) FlowCalculator() TrafficShapingCalculator {
	return t.flowCalculator
}

// PerformChecking 流量校验
func (t *TrafficShapingController) PerformChecking(resStat base.StatNode, batchCount uint32, flag int32) *base.TokenResult {
	// 获取流量控制阀值
	allowedTokens := t.flowCalculator.CalculateAllowedTokens(batchCount, flag)
	metrics.SetResourceFlowThreshold(t.rule.Resource, allowedTokens)
	// 获取当前流量进行比较
	return t.flowChecker.DoCheck(resStat, batchCount, allowedTokens)
}

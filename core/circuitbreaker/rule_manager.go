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
	"reflect"
	"sync"

	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

type CircuitBreakerGenFunc func(r *Rule, reuseStat interface{}) (CircuitBreaker, error)

var (
	cbGenFuncMap = make(map[Strategy]CircuitBreakerGenFunc, 4)

	breakerRules  = make(map[string][]*Rule)          // 当前生效的规则 rule
	breakers      = make(map[string][]CircuitBreaker) // 当前生效的每个规则对应的breaker
	updateMux     = new(sync.RWMutex)
	currentRules  = make(map[string][]*Rule, 0) // 当前所有的规则
	updateRuleMux = new(sync.Mutex)

	stateChangeListeners = make([]StateChangeListener, 0) // 熔断状态变更的监听器
)

// 不同的熔断策略对应的breaker创建，rule规则，reuseStat统计信息
func init() {
	cbGenFuncMap[SlowRequestRatio] = func(r *Rule, reuseStat interface{}) (CircuitBreaker, error) {
		if r == nil {
			return nil, errors.New("nil rule")
		}
		if reuseStat == nil {
			return newSlowRtCircuitBreaker(r) // 创建stat后再创建breaker
		}

		stat, ok := reuseStat.(*slowRequestLeapArray) // reuseStat不为nil时的校验
		if !ok || stat == nil {
			logging.Warn("[CircuitBreaker RuleManager] Expect to generate circuit breaker with reuse statistic, but fail to do type assertion, expect:*slowRequestLeapArray", "statType", reflect.TypeOf(stat).Name())
			return newSlowRtCircuitBreaker(r)
		}
		return newSlowRtCircuitBreakerWithStat(r, stat), nil
	}

	cbGenFuncMap[ErrorRatio] = func(r *Rule, reuseStat interface{}) (CircuitBreaker, error) {
		if r == nil {
			return nil, errors.New("nil rule")
		}
		if reuseStat == nil {
			return newErrorRatioCircuitBreaker(r)
		}
		stat, ok := reuseStat.(*errorCounterLeapArray)
		if !ok || stat == nil {
			logging.Warn("[CircuitBreaker RuleManager] Expect to generate circuit breaker with reuse statistic, but fail to do type assertion, expect:*errorCounterLeapArray", "statType", reflect.TypeOf(stat).Name())
			return newErrorRatioCircuitBreaker(r)
		}
		return newErrorRatioCircuitBreakerWithStat(r, stat), nil
	}

	cbGenFuncMap[ErrorCount] = func(r *Rule, reuseStat interface{}) (CircuitBreaker, error) {
		if r == nil {
			return nil, errors.New("nil rule")
		}
		if reuseStat == nil {
			return newErrorCountCircuitBreaker(r)
		}
		stat, ok := reuseStat.(*errorCounterLeapArray)
		if !ok || stat == nil {
			logging.Warn("[CircuitBreaker RuleManager] Expect to generate circuit breaker with reuse statistic, but fail to do type assertion, expect:*errorCounterLeapArray", "statType", reflect.TypeOf(stat).Name())
			return newErrorCountCircuitBreaker(r)
		}
		return newErrorCountCircuitBreakerWithStat(r, stat), nil
	}
}

// GetRulesOfResource returns specific resource's rules based on copy.
// It doesn't take effect for circuit breaker module if user changes the rule.
// GetRulesOfResource need to compete circuit breaker module's global lock and the high performance losses of copy,
// 		reduce or do not call GetRulesOfResource frequently if possible
// GetRulesOfResource返回基于复制的特定资源的规则。如果用户修改规则，则不生效。
// 需要全局锁和复制的高性能损失， 如果可能的话，减少或不频繁调用GetRulesOfResource
// 指定resource的规则
func GetRulesOfResource(resource string) []Rule {
	updateMux.RLock()
	resRules, ok := breakerRules[resource]
	updateMux.RUnlock()
	if !ok {
		return nil
	}
	ret := make([]Rule, 0, len(resRules))
	for _, rule := range resRules {
		ret = append(ret, *rule)
	}
	return ret
}

// GetRules returns all the rules based on copy.
// It doesn't take effect for circuit breaker module if user changes the rule.
// GetRules need to compete circuit breaker module's global lock and the high performance losses of copy,
// 		reduce or do not call GetRules if possible
// GetRules返回所有基于copy的规则。如果用户修改规则，则不生效。
// 需要竞争断路器模块的全局锁和复制的高性能损失，如果可能的话，减少或不调用GetRules
func GetRules() []Rule {
	updateMux.RLock()
	rules := rulesFrom(breakerRules)
	updateMux.RUnlock()
	ret := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		ret = append(ret, *rule)
	}
	return ret
}

// ClearRules clear all the previous rules.
// 清除所有以前的规则
func ClearRules() error {
	_, err := LoadRules(nil)
	return err
}

// LoadRules replaces old rules with the given circuit breaking rules.
//
// return value:
//
// bool: was designed to indicate whether the internal map has been changed
// error: was designed to indicate whether occurs the error.
// LoadRules用给定的断路规则替换旧规则。返回值: bool:用于指示内部映射是否已更改，错误:被设计用来指示是否发生错误。
func LoadRules(rules []*Rule) (bool, error) {
	resRulesMap := make(map[string][]*Rule, 16)
	for _, rule := range rules {
		resRules, exist := resRulesMap[rule.Resource]
		if !exist {
			resRules = make([]*Rule, 0, 1)
		}
		resRulesMap[rule.Resource] = append(resRules, rule)
	}

	// 是否相同
	updateRuleMux.Lock()
	defer updateRuleMux.Unlock()
	isEqual := reflect.DeepEqual(currentRules, resRulesMap)
	if isEqual {
		logging.Info("[CircuitBreaker] Load rules is the same with current rules, so ignore load operation.")
		return false, nil
	}

	// 更新
	err := onRuleUpdate(resRulesMap)
	return true, err
}

// LoadRulesOfResource loads the given resource's circuitBreaker rules to the rule manager, while all previous resource's rules will be replaced.
// the first returned value indicates whether do real load operation, if the rules is the same with previous resource's rules, return false
// LoadRulesOfResource将给定资源的circuitBreaker规则加载到规则管理器中，而之前所有资源的规则将被替换。
// 第一个返回值指示是否进行实际的加载操作，如果规则与前一个资源的规则相同，则返回false
// 更新指定resource的rule
func LoadRulesOfResource(res string, rules []*Rule) (bool, error) {
	if len(res) == 0 {
		return false, errors.New("empty resource")
	}
	updateRuleMux.Lock()
	defer updateRuleMux.Unlock()
	// clear resource rules
	// 删除
	if len(rules) == 0 {
		// clear resource's currentRules
		delete(currentRules, res)
		// clear breakers & breakerRules
		updateMux.Lock()
		delete(breakers, res)
		delete(breakerRules, res)
		updateMux.Unlock()
		logging.Info("[CircuitBreaker] clear resource level rules", "resource", res)
		return true, nil
	}

	// load resource level rules
	// 是否相等
	isEqual := reflect.DeepEqual(currentRules[res], rules)
	if isEqual {
		logging.Info("[CircuitBreaker] Load resource level rules is the same with current resource level rules, so ignore load operation.")
		return false, nil
	}
	err := onResourceRuleUpdate(res, rules)
	return true, err
}

// 获取指定resource的breaker
func getBreakersOfResource(resource string) []CircuitBreaker {
	updateMux.RLock()
	resCBs := breakers[resource]
	updateMux.RUnlock()
	ret := make([]CircuitBreaker, 0, len(resCBs))
	if len(resCBs) == 0 {
		return ret
	}
	ret = append(ret, resCBs...)
	return ret
}

// 从旧有的breaker中匹配rule
func calculateReuseIndexFor(r *Rule, oldResCbs []CircuitBreaker) (equalIdx, reuseStatIdx int) {
	// the index of equivalent rule in old circuit breaker slice
	// 相等的下标
	equalIdx = -1
	// the index of statistic reusable rule in old circuit breaker slice
	// 可重用的下标
	reuseStatIdx = -1

	for idx, oldTc := range oldResCbs {
		oldRule := oldTc.BoundRule()
		// 与旧rule相等
		if oldRule.isEqualsTo(r) {
			// break if there is equivalent rule
			equalIdx = idx
			break
		}

		// find the index of first StatReusable rule
		// 第一个能重用的规则
		if !oldRule.isStatReusable(r) {
			continue
		}

		// 已经找到能重用的规则
		if reuseStatIdx >= 0 {
			// had find reuse rule.
			continue
		}
		reuseStatIdx = idx
	}
	return equalIdx, reuseStatIdx
}

// Concurrent safe to update rules
// 并发安全更新规则
func onRuleUpdate(rawResRulesMap map[string][]*Rule) (err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("%+v", r)
			}
		}
	}()
	// ignore invalid rules
	// 忽略无效的规则
	validResRulesMap := make(map[string][]*Rule, len(rawResRulesMap))
	for res, rules := range rawResRulesMap {
		validResRules := make([]*Rule, 0, len(rules))
		for _, rule := range rules {
			if err := IsValidRule(rule); err != nil {
				logging.Warn("[CircuitBreaker onRuleUpdate] Ignoring invalid circuit breaking rule when loading new rules", "rule", rule, "err", err.Error())
				continue
			}
			validResRules = append(validResRules, rule)
		}
		if len(validResRules) > 0 {
			validResRulesMap[res] = validResRules
		}
	}

	start := util.CurrentTimeNano()

	// 复制旧规则的breaker
	updateMux.RLock()
	breakersClone := make(map[string][]CircuitBreaker, len(validResRulesMap))
	for res, tcs := range breakers {
		resTcClone := make([]CircuitBreaker, 0, len(tcs))
		resTcClone = append(resTcClone, tcs...)
		breakersClone[res] = resTcClone
	}
	updateMux.RUnlock()

	// 对有效的新规则创建breaker
	newBreakers := make(map[string][]CircuitBreaker, len(validResRulesMap))
	for res, resRules := range validResRulesMap {
		newCbsOfRes := buildResourceCircuitBreaker(res, resRules, breakersClone[res]) // 创建
		if len(newCbsOfRes) > 0 {
			newBreakers[res] = newCbsOfRes
		}
	}

	// 更新
	updateMux.Lock()
	breakerRules = validResRulesMap
	breakers = newBreakers
	updateMux.Unlock()
	currentRules = rawResRulesMap

	logging.Debug("[CircuitBreaker onRuleUpdate] Time statistics(ns) for updating circuit breaker rule", "timeCost", util.CurrentTimeNano()-start)
	logRuleUpdate(validResRulesMap)
	return nil
}

// 更新指定resource的rule的breaker
func onResourceRuleUpdate(res string, rawResRules []*Rule) (err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("%v", r)
			}
		}
	}()

	// 是否有效
	validResRules := make([]*Rule, 0, len(rawResRules))
	for _, rule := range rawResRules {
		if err := IsValidRule(rule); err != nil {
			logging.Warn("[CircuitBreaker onResourceRuleUpdate] Ignoring invalid circuitBreaker rule", "rule", rule, "reason", err.Error())
			continue
		}
		validResRules = append(validResRules, rule)
	}

	// 复制旧breaker
	start := util.CurrentTimeNano()
	oldResCbs := make([]CircuitBreaker, 0)
	updateMux.RLock()
	oldResCbs = append(oldResCbs, breakers[res]...)
	updateMux.RUnlock()

	// 创建
	newCbsOfRes := buildResourceCircuitBreaker(res, rawResRules, oldResCbs)

	// 更新
	updateMux.Lock()
	if len(newCbsOfRes) == 0 {
		delete(breakerRules, res)
		delete(breakers, res)
	} else {
		breakerRules[res] = validResRules
		breakers[res] = newCbsOfRes
	}
	updateMux.Unlock()
	currentRules[res] = rawResRules

	logging.Debug("[CircuitBreaker onResourceRuleUpdate] Time statistics(ns) for updating circuit breaker rule", "timeCost", util.CurrentTimeNano()-start)
	logging.Info("[CircuitBreaker] load resource level rules", "resource", res, "validResRules", validResRules)
	return nil
}

// map转成数组
func rulesFrom(rm map[string][]*Rule) []*Rule {
	rules := make([]*Rule, 0, 8)
	if len(rm) == 0 {
		return rules
	}
	for _, rs := range rm {
		if len(rs) == 0 {
			continue
		}
		for _, r := range rs {
			if r != nil {
				rules = append(rules, r)
			}
		}
	}
	return rules
}

// 日志记录
func logRuleUpdate(m map[string][]*Rule) {
	rs := rulesFrom(m)
	if len(rs) == 0 {
		logging.Info("[CircuitBreakerRuleManager] Circuit breaking rules were cleared")
	} else {
		logging.Info("[CircuitBreakerRuleManager] Circuit breaking rules were loaded", "rules", rs)
	}
}

// RegisterStateChangeListeners registers the global state change listener for all circuit breakers
// Note: this function is not thread-safe.
func RegisterStateChangeListeners(listeners ...StateChangeListener) {
	if len(listeners) == 0 {
		return
	}

	stateChangeListeners = append(stateChangeListeners, listeners...)
}

// ClearStateChangeListeners clears the all StateChangeListener
// Note: this function is not thread-safe.
func ClearStateChangeListeners() {
	stateChangeListeners = make([]StateChangeListener, 0)
}

// SetCircuitBreakerGenerator sets the circuit breaker generator for the given strategy.
// Note that modifying the generator of default strategies is not allowed.
// 动态设置策略对应的breaker 不允许更改默认的
func SetCircuitBreakerGenerator(s Strategy, generator CircuitBreakerGenFunc) error {
	if generator == nil {
		return errors.New("nil generator")
	}
	if s <= ErrorCount {
		return errors.New("not allowed to replace the generator for default circuit breaking strategies")
	}
	updateMux.Lock()
	defer updateMux.Unlock()

	cbGenFuncMap[s] = generator
	return nil
}

// RemoveCircuitBreakerGenerator 删除动态添加的策略对应的breaker 不允许修改默认的
func RemoveCircuitBreakerGenerator(s Strategy) error {
	if s <= ErrorCount {
		return errors.New("not allowed to remove the generator for default circuit breaking strategies")
	}
	updateMux.Lock()
	defer updateMux.Unlock()

	delete(cbGenFuncMap, s)
	return nil
}

// ClearRulesOfResource clears resource level rules in circuitBreaker module.
// 清理指定resource的rule
func ClearRulesOfResource(res string) error {
	_, err := LoadRulesOfResource(res, nil)
	return err
}

// buildResourceCircuitBreaker builds CircuitBreaker slice from rules. the resource of rules must be equals to res
// 对resource的rules创建breaker
func buildResourceCircuitBreaker(res string, rulesOfRes []*Rule, oldResCbs []CircuitBreaker) []CircuitBreaker {
	newCbsOfRes := make([]CircuitBreaker, 0, len(rulesOfRes))
	for _, r := range rulesOfRes {
		if res != r.Resource {
			logging.Error(errors.Errorf("unmatched resource name expect: %s, actual: %s", res, r.Resource), "Unmatched resource name in circuitBreaker.buildResourceCircuitBreaker()", "rule", r)
			continue
		}
		equalIdx, reuseStatIdx := calculateReuseIndexFor(r, oldResCbs)

		// First check equals scenario
		// 第一个检查等于场景
		if equalIdx >= 0 {
			// reuse the old cb
			// 重用旧的breaker
			equalOldCb := oldResCbs[equalIdx]
			newCbsOfRes = append(newCbsOfRes, equalOldCb)
			// remove old cb from oldResCbs
			// 删除旧值
			oldResCbs = append(oldResCbs[:equalIdx], oldResCbs[equalIdx+1:]...)
			continue
		}

		generator := cbGenFuncMap[r.Strategy]
		if generator == nil {
			logging.Warn("[CircuitBreaker buildResourceCircuitBreaker] Ignoring the rule due to unsupported circuit breaking strategy", "rule", r)
			continue
		}

		var cb CircuitBreaker
		var e error
		if reuseStatIdx >= 0 { // 重用统计信息
			cb, e = generator(r, oldResCbs[reuseStatIdx].BoundStat())
		} else {
			cb, e = generator(r, nil) // 完全新建
		}
		if cb == nil || e != nil {
			logging.Warn("[CircuitBreaker buildResourceCircuitBreaker] Ignoring the rule due to bad generated circuit breaker", "rule", r, "err", e.Error())
			continue
		}

		// 删除重用的breaker
		if reuseStatIdx >= 0 {
			oldResCbs = append(oldResCbs[:reuseStatIdx], oldResCbs[reuseStatIdx+1:]...)
		}
		newCbsOfRes = append(newCbsOfRes, cb)
	}
	return newCbsOfRes
}

// IsValidRule 规则是否有效
func IsValidRule(r *Rule) error {
	if r == nil {
		return errors.New("nil Rule")
	}
	if len(r.Resource) == 0 {
		return errors.New("empty resource name")
	}
	if r.StatIntervalMs <= 0 {
		return errors.New("invalid StatIntervalMs")
	}
	if r.RetryTimeoutMs <= 0 {
		return errors.New("invalid RetryTimeoutMs")
	}
	if r.Threshold < 0.0 {
		return errors.New("invalid Threshold")
	}
	if r.Strategy == SlowRequestRatio && r.Threshold > 1.0 {
		return errors.New("invalid slow request ratio threshold (valid range: [0.0, 1.0])")
	}
	if r.Strategy == ErrorRatio && r.Threshold > 1.0 {
		return errors.New("invalid error ratio threshold (valid range: [0.0, 1.0])")
	}
	if r.StatSlidingWindowBucketCount != 0 && r.StatIntervalMs%r.StatSlidingWindowBucketCount != 0 {
		logging.Warn("[CircuitBreaker IsValidRule] The following must be true: StatIntervalMs % StatSlidingWindowBucketCount == 0. StatSlidingWindowBucketCount will be replaced by 1", "rule", r)
	}
	return nil
}

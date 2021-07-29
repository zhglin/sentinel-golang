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

package system

import (
	"reflect"
	"sync"

	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

type RuleMap map[MetricType][]*Rule

// const
var (
	ruleMap       = make(RuleMap) // map的
	ruleMapMux    = new(sync.RWMutex)
	currentRules  = make([]*Rule, 0) // slice的
	updateRuleMux = new(sync.Mutex)
)

// GetRules returns all the rules based on copy.
// It doesn't take effect for system module if user changes the rule.
// GetRules need to compete system module's global lock and the high performance losses of copy,
// 		reduce or do not call GetRules if possible
// GetRules返回所有基于copy的规则。如果用户修改了规则，它对系统模块不生效。
// GetRules需要竞争系统模块的全局锁和复制的高性能损失，如果可能的话，减少或不调用GetRules
func GetRules() []Rule {
	rules := make([]*Rule, 0, len(ruleMap))
	ruleMapMux.RLock()
	for _, rs := range ruleMap {
		rules = append(rules, rs...)
	}
	ruleMapMux.RUnlock()

	ret := make([]Rule, 0, len(rules))
	for _, r := range rules {
		ret = append(ret, *r)
	}
	return ret
}

// getRules returns all the rules。Any changes of rules take effect for system module
// getRules is an internal interface.
// 返回所有规则。规则的任何修改均对系统模块生效
// getRules是一个内部接口。map改成slice
func getRules() []*Rule {
	ruleMapMux.RLock()
	defer ruleMapMux.RUnlock()

	rules := make([]*Rule, 0, 8)
	for _, rs := range ruleMap {
		rules = append(rules, rs...)
	}
	return rules
}

// LoadRules loads given system rules to the rule manager, while all previous rules will be replaced.
// 将给定的系统规则加载到规则管理器，而之前的所有规则将被替换。
func LoadRules(rules []*Rule) (bool, error) {
	updateRuleMux.Lock()
	defer updateRuleMux.Unlock()
	isEqual := reflect.DeepEqual(currentRules, rules) // 新老是否一样
	if isEqual {
		logging.Info("[System] Load rules is the same with current rules, so ignore load operation.")
		return false, nil
	}

	m := buildRuleMap(rules)

	if err := onRuleUpdate(m); err != nil {
		logging.Error(err, "Fail to load rules in system.LoadRules()", "rules", rules)
		return false, err
	}
	currentRules = rules // 设置currentRules
	return true, nil
}

// ClearRules clear all the previous rules
func ClearRules() error {
	_, err := LoadRules(nil)
	return err
}

// 重置rule
func onRuleUpdate(r RuleMap) error {
	start := util.CurrentTimeNano()
	ruleMapMux.Lock()
	ruleMap = r
	ruleMapMux.Unlock()

	logging.Debug("[System onRuleUpdate] Time statistic(ns) for updating system rule", "timeCost", util.CurrentTimeNano()-start)
	if len(r) > 0 {
		logging.Info("[SystemRuleManager] System rules loaded", "rules", r)
	} else {
		logging.Info("[SystemRuleManager] System rules were cleared")
	}
	return nil
}

// 校验并创建RuleMap
func buildRuleMap(rules []*Rule) RuleMap {
	m := make(RuleMap)

	if len(rules) == 0 {
		return m
	}

	for _, rule := range rules {
		// 校验rule
		if err := IsValidSystemRule(rule); err != nil {
			logging.Warn("[System buildRuleMap] Ignoring invalid system rule", "rule", rule, "err", err.Error())
			continue
		}
		// 是否有同类型的
		rulesOfRes, exists := m[rule.MetricType]
		if !exists {
			m[rule.MetricType] = []*Rule{rule}
		} else {
			m[rule.MetricType] = append(rulesOfRes, rule) // 相同类型的
		}
	}
	return m
}

// IsValidSystemRule determine the system rule is valid or not
// 判断系统规则是否有效
func IsValidSystemRule(rule *Rule) error {
	if rule == nil {
		return errors.New("nil Rule")
	}
	if rule.TriggerCount < 0 {
		return errors.New("negative threshold")
	}
	if rule.MetricType >= MetricTypeSize {
		return errors.New("invalid metric type")
	}

	if rule.MetricType == CpuUsage && rule.TriggerCount > 1 {
		return errors.New("invalid CPU usage, valid range is [0.0, 1.0]")
	}
	return nil
}

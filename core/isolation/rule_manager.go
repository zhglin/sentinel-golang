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

package isolation

import (
	"reflect"
	"sync"

	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

var (
	ruleMap       = make(map[string][]*Rule) // 当前生效的有效规则
	rwMux         = &sync.RWMutex{}
	currentRules  = make(map[string][]*Rule, 0) // 当前配置的全部规则
	updateRuleMux = new(sync.Mutex)
)

// LoadRules loads the given isolation rules to the rule manager, while all previous rules will be replaced.
// the first returned value indicates whether do real load operation, if the rules is the same with previous rules, return false
// LoadRules将给定的隔离规则加载到规则管理器，而之前的所有规则将被替换。
// 第一个返回值指示是否进行实际加载操作，如果规则与前面的规则相同，则返回false
func LoadRules(rules []*Rule) (bool, error) {
	resRulesMap := make(map[string][]*Rule, 16)
	for _, rule := range rules {
		resRules, exist := resRulesMap[rule.Resource]
		if !exist {
			resRules = make([]*Rule, 0, 1)
		}
		resRulesMap[rule.Resource] = append(resRules, rule)
	}

	updateRuleMux.Lock()
	defer updateRuleMux.Unlock()
	isEqual := reflect.DeepEqual(currentRules, resRulesMap)
	if isEqual {
		logging.Info("[Isolation] Load rules is the same with current rules, so ignore load operation.")
		return false, nil
	}

	err := onRuleUpdate(resRulesMap)
	return true, err
}

// 生效新规则
func onRuleUpdate(rawResRulesMap map[string][]*Rule) (err error) {
	validResRulesMap := make(map[string][]*Rule, len(rawResRulesMap))
	for res, rules := range rawResRulesMap {
		validResRules := make([]*Rule, 0, len(rules))
		for _, rule := range rules {
			if err := IsValidRule(rule); err != nil {
				logging.Warn("[Isolation onRuleUpdate] Ignoring invalid isolation rule", "rule", rule, "reason", err.Error())
				continue
			}
			validResRules = append(validResRules, rule)
		}
		if len(validResRules) > 0 {
			validResRulesMap[res] = validResRules
		}
	}

	// 替换新规则
	start := util.CurrentTimeNano()
	rwMux.Lock()
	ruleMap = validResRulesMap
	rwMux.Unlock()
	currentRules = rawResRulesMap

	logging.Debug("[Isolation onRuleUpdate] Time statistic(ns) for updating isolation rule", "timeCost", util.CurrentTimeNano()-start)
	logRuleUpdate(validResRulesMap)
	return
}

// LoadRulesOfResource loads the given resource's isolation rules to the rule manager, while all previous resource's rules will be replaced.
// the first returned value indicates whether do real load operation, if the rules is the same with previous resource's rules, return false
// LoadRulesOfResource将给定资源的隔离规则加载到规则管理器，而之前所有资源的规则将被替换。
// 第一个返回值指示是否进行实际的加载操作，如果规则与前一个资源的规则相同，则返回false
// 指定的resource
func LoadRulesOfResource(res string, rules []*Rule) (bool, error) {
	if len(res) == 0 {
		return false, errors.New("empty resource")
	}
	updateRuleMux.Lock()
	defer updateRuleMux.Unlock()
	// clear resource rules
	if len(rules) == 0 {
		// clear resource's currentRules 清空
		delete(currentRules, res)
		// clear ruleMap
		rwMux.Lock()
		delete(ruleMap, res)
		rwMux.Unlock()
		logging.Info("[Isolation] clear resource level rules", "resource", res)
		return true, nil
	}
	// load resource level rules
	isEqual := reflect.DeepEqual(currentRules[res], rules)
	if isEqual {
		logging.Info("[Isolation] Load resource level rules is the same with current resource level rules, so ignore load operation.")
		return false, nil
	}

	err := onResourceRuleUpdate(res, rules)
	return true, err
}

// 生效指定resource的规则
func onResourceRuleUpdate(res string, rawResRules []*Rule) (err error) {
	// 校验
	validResRules := make([]*Rule, 0, len(rawResRules))
	for _, rule := range rawResRules {
		if err := IsValidRule(rule); err != nil {
			logging.Warn("[Isolation onResourceRuleUpdate] Ignoring invalid isolation rule", "rule", rule, "reason", err.Error())
			continue
		}
		validResRules = append(validResRules, rule)
	}

	start := util.CurrentTimeNano()
	rwMux.Lock()
	if len(validResRules) == 0 {
		delete(ruleMap, res)
	} else {
		ruleMap[res] = validResRules
	}
	rwMux.Unlock()
	currentRules[res] = rawResRules
	logging.Debug("[Isolation onResourceRuleUpdate] Time statistic(ns) for updating isolation rule", "timeCost", util.CurrentTimeNano()-start)
	logging.Info("[Isolation] load resource level rules", "resource", res, "validResRules", validResRules)
	return nil
}

// ClearRules clears all the rules in isolation module.
// 清除所有规则
func ClearRules() error {
	_, err := LoadRules(nil)
	return err
}

// ClearRulesOfResource clears resource level rules in isolation module.
// 清除指定resource的规则
func ClearRulesOfResource(res string) error {
	_, err := LoadRulesOfResource(res, nil)
	return err
}

// GetRules returns all the rules based on copy.
// It doesn't take effect for isolation module if user changes the rule.
// 返回基于复制的所有规则。如果用户更改了规则，它对隔离模块不起作用。
func GetRules() []Rule {
	rules := getRules()
	ret := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		ret = append(ret, *rule)
	}
	return ret
}

// GetRulesOfResource returns specific resource's rules based on copy.
// It doesn't take effect for isolation module if user changes the rule.
// GetRulesOfResource返回基于复制的特定资源的规则。
// 如果用户更改了规则，它对隔离模块不起作用。
func GetRulesOfResource(res string) []Rule {
	rules := getRulesOfResource(res)
	ret := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		ret = append(ret, *rule)
	}
	return ret
}

// getRules returns all the rules。Any changes of rules take effect for isolation module
// getRules is an internal interface.
// getRules返回所有规则。
// 规则的任何更改都对隔离模块生效，getRules是内部接口。
func getRules() []*Rule {
	rwMux.RLock()
	defer rwMux.RUnlock()

	return rulesFrom(ruleMap)
}

// getRulesOfResource returns specific resource's rules。Any changes of rules take effect for isolation module
// getRulesOfResource is an internal interface.
// getRulesOfResource返回特定资源的规则。
// 规则的任何更改都会因隔离模块getRulesOfResource是内部接口而生效。
func getRulesOfResource(res string) []*Rule {
	rwMux.RLock()
	defer rwMux.RUnlock()

	resRules, exist := ruleMap[res]
	if !exist {
		return nil
	}
	ret := make([]*Rule, 0, len(resRules))
	for _, r := range resRules {
		ret = append(ret, r)
	}
	return ret
}

// 转成数组返回
func rulesFrom(m map[string][]*Rule) []*Rule {
	rules := make([]*Rule, 0, 8)
	if len(m) == 0 {
		return rules
	}
	for _, rs := range m {
		for _, r := range rs {
			if r != nil {
				rules = append(rules, r)
			}
		}
	}
	return rules
}

// 记录日志
func logRuleUpdate(m map[string][]*Rule) {
	rs := rulesFrom(m)
	if len(rs) == 0 {
		logging.Info("[IsolationRuleManager] Isolation rules were cleared")
	} else {
		logging.Info("[IsolationRuleManager] Isolation rules were loaded", "rules", rs)
	}
}

// IsValidRule checks whether the given Rule is valid.
// 检查给定的规则是否有效。
func IsValidRule(r *Rule) error {
	if r == nil {
		return errors.New("nil isolation rule")
	}
	if len(r.Resource) == 0 {
		return errors.New("empty resource of isolation rule")
	}
	if r.MetricType != Concurrency {
		return errors.Errorf("unsupported metric type: %d", r.MetricType)
	}
	if r.Threshold == 0 {
		return errors.New("zero threshold")
	}
	return nil
}

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

package base

import (
	"sort"
	"sync"

	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

type BaseSlot interface {
	// Order returns the sort value of the slot.
	// 返回槽的排序值。
	// SlotChain will sort all it's slots by ascending sort value in each bucket
	// 将排序它的所有槽的升序值在每个桶
	// (StatPrepareSlot bucket、RuleCheckSlot bucket and StatSlot bucket)
	Order() uint32
}

// StatPrepareSlot is responsible for some preparation before statistic
// For example: init structure and so on
// 负责统计前的一些准备工作，
type StatPrepareSlot interface {
	BaseSlot
	// Prepare function do some initialization
	// Such as: init statistic structure、node and etc
	// The result of preparing would store in EntryContext
	// All StatPrepareSlots execute in sequence
	// Prepare function should not throw panic.
	// 准备的结果将存储在EntryContext中。所有的StatPrepareSlots将依次执行，不应触发panic
	Prepare(ctx *EntryContext)
}

// RuleCheckSlot is rule based checking strategy
// All checking rule must implement this interface.
// 基于规则的检查策略 所有的检查规则都必须实现此接口。
type RuleCheckSlot interface {
	BaseSlot
	// Check function do some validation
	// It can break off the slot pipeline
	// Each TokenResult will return check result
	// The upper logic will control pipeline according to SlotResult.
	Check(ctx *EntryContext) *TokenResult
}

// StatSlot is responsible for counting all custom biz metrics.
// StatSlot would not handle any panic, and pass up all panic to slot chain
// StatSlot负责统计所有定制业务指标。
// StatSlot不会处理任何恐慌，并将所有恐慌传递给槽链
type StatSlot interface {
	BaseSlot
	// OnEntryPassed OnEntryPass function will be invoked when StatPrepareSlots and RuleCheckSlots execute pass
	// StatSlots will do some statistic logic, such as QPS、log、etc
	// 当StatPrepareSlots和RuleCheckSlots执行时，OnEntryPass函数将被调用，而StatSlots将执行一些统计逻辑，如QPS、log等
	OnEntryPassed(ctx *EntryContext)
	// OnEntryBlocked function will be invoked when StatPrepareSlots and RuleCheckSlots fail to execute
	// It may be inbound flow control or outbound cir
	// StatSlots will do some statistic logic, such as QPS、log、etc
	// blockError introduce the block detail
	// 当StatPrepareSlots和RuleCheckSlots执行失败时，OnEntryBlocked函数将被调用。它可能是入站流控制或出站cir。StatSlots将执行一些统计逻辑，如QPS, log等
	OnEntryBlocked(ctx *EntryContext, blockError *BlockError)
	// OnCompleted function will be invoked when chain exits.
	// The semantics of OnCompleted is the entry passed and completed
	// Note: blocked entry will not call this function
	// 当链退出时将调用OnCompleted函数。注意:被阻塞的条目将不会调用这个函数
	OnCompleted(ctx *EntryContext)
}

// SlotChain hold all system slots and customized slot.
// SlotChain support plug-in slots developed by developer.
// SlotChain包含所有系统槽位和自定义槽位。
// SlotChain支持开发人员开发的插件槽。
type SlotChain struct {
	// statPres is in ascending order by StatPrepareSlot.Order() value.
	// statPres按StatPrepareSlot.Order()值升序排列。
	statPres []StatPrepareSlot
	// ruleChecks is in ascending order by RuleCheckSlot.Order() value.
	// ruleChecks按照RuleCheckSlot.Order()值升序排列。
	ruleChecks []RuleCheckSlot
	// stats is in ascending order by StatSlot.Order() value.
	// stats按StatSlot.Order()值升序排列。
	stats []StatSlot
	// EntryContext Pool, used for reuse EntryContext object
	// EntryContext内存池，用于重用EntryContext对象
	ctxPool *sync.Pool
}

var (
	// 返回默认的空EntryContext
	ctxPool = &sync.Pool{
		New: func() interface{} {
			ctx := NewEmptyEntryContext()
			ctx.RuleCheckResult = NewTokenResultPass()
			ctx.Data = make(map[interface{}]interface{})
			ctx.Input = &SentinelInput{
				BatchCount:  1,
				Flag:        0,
				Args:        make([]interface{}, 0),
				Attachments: make(map[interface{}]interface{}),
			}
			return ctx
		},
	}
)

// NewSlotChain 不同的规则对应的不同的函数链，BaseSlot是函数链的调用顺序
func NewSlotChain() *SlotChain {
	return &SlotChain{
		statPres:   make([]StatPrepareSlot, 0, 8),
		ruleChecks: make([]RuleCheckSlot, 0, 8),
		stats:      make([]StatSlot, 0, 8),
		ctxPool:    ctxPool,
	}
}

// GetPooledContext Get a EntryContext from EntryContext ctxPool, if ctxPool doesn't have enough EntryContext then new one.
// 从内存池中获取EntryContext对象
func (sc *SlotChain) GetPooledContext() *EntryContext {
	ctx := sc.ctxPool.Get().(*EntryContext)
	ctx.startTime = util.CurrentTimeMillis() // 设置开始时间
	return ctx
}

// RefurbishContext 重置EntryContext并返回到内存池
func (sc *SlotChain) RefurbishContext(c *EntryContext) {
	if c != nil {
		c.Reset()
		sc.ctxPool.Put(c)
	}
}

// AddStatPrepareSlot adds the StatPrepareSlot slot to the StatPrepareSlot list of the SlotChain.
// All StatPrepareSlot in the list will be sorted according to StatPrepareSlot.Order() in ascending order.
// AddStatPrepareSlot is non-thread safe,
// In concurrency scenario, AddStatPrepareSlot must be guarded by SlotChain.RWMutex#Lock
// AddStatPrepareSlot将StatPrepareSlot添加到SlotChain的StatPrepareSlot列表中。
// 列表中的所有StatPrepareSlot将按照StatPrepareSlot.order()的升序排序。
// AddStatPrepareSlot是非线程安全的，
// 在并发场景中，AddStatPrepareSlot必须由SlotChain保护。RWMutex #锁
func (sc *SlotChain) AddStatPrepareSlot(s StatPrepareSlot) {
	sc.statPres = append(sc.statPres, s)
	sort.SliceStable(sc.statPres, func(i, j int) bool {
		return sc.statPres[i].Order() < sc.statPres[j].Order()
	})
}

// AddRuleCheckSlot adds the RuleCheckSlot to the RuleCheckSlot list of the SlotChain.
// All RuleCheckSlot in the list will be sorted according to RuleCheckSlot.Order() in ascending order.
// AddRuleCheckSlot is non-thread safe,
// In concurrency scenario, AddRuleCheckSlot must be guarded by SlotChain.RWMutex#Lock
// AddRuleCheckSlot将RuleCheckSlot添加到SlotChain的RuleCheckSlot列表中。
// 列表中的所有RuleCheckSlot将按照RuleCheckSlot.order()的升序排序。
// AddRuleCheckSlot是非线程安全的，
// 在并发场景中，AddRuleCheckSlot必须由SlotChain保护。RWMutex #锁
func (sc *SlotChain) AddRuleCheckSlot(s RuleCheckSlot) {
	sc.ruleChecks = append(sc.ruleChecks, s)
	sort.SliceStable(sc.ruleChecks, func(i, j int) bool {
		return sc.ruleChecks[i].Order() < sc.ruleChecks[j].Order()
	})
}

// AddStatSlot adds the StatSlot to the StatSlot list of the SlotChain.
// All StatSlot in the list will be sorted according to StatSlot.Order() in ascending order.
// AddStatSlot is non-thread safe,
// In concurrency scenario, AddStatSlot must be guarded by SlotChain.RWMutex#Lock
// AddStatSlot将StatSlot添加到SlotChain的StatSlot列表中。
// 列表中的所有StatSlot将按照StatSlot. order()的升序排序。
// AddStatSlot是非线程安全的，
// 在并发场景中，AddStatSlot必须由SlotChain保护。RWMutex #锁
func (sc *SlotChain) AddStatSlot(s StatSlot) {
	sc.stats = append(sc.stats, s)
	sort.SliceStable(sc.stats, func(i, j int) bool {
		return sc.stats[i].Order() < sc.stats[j].Order()
	})
}

// Entry The entrance of slot chain
// Return the TokenResult and nil if internal panic.
// 对EntryContext应用所有函数链中的函数
func (sc *SlotChain) Entry(ctx *EntryContext) *TokenResult {
	// This should not happen, unless there are errors existing in Sentinel internal.
	// If happened, need to add TokenResult in EntryContext
	// 这应该不会发生，除非Sentinel内部存在错误。如果发生，需要在EntryContext中添加TokenResult
	defer func() {
		if err := recover(); err != nil {
			logging.Error(errors.Errorf("%+v", err), "Sentinel internal panic in SlotChain.Entry()")
			ctx.SetError(errors.Errorf("%+v", err))
			return
		}
	}()

	// execute prepare slot
	// 执行预处理slot
	sps := sc.statPres
	if len(sps) > 0 {
		for _, s := range sps {
			s.Prepare(ctx)
		}
	}

	// execute rule based checking slot
	// 执行规则校验slot
	rcs := sc.ruleChecks
	var ruleCheckRet *TokenResult
	if len(rcs) > 0 {
		for _, s := range rcs {
			sr := s.Check(ctx)
			if sr == nil {
				// nil equals to check pass
				continue
			}
			// check slot result
			if sr.IsBlocked() {
				ruleCheckRet = sr
				break
			}
		}
	}
	if ruleCheckRet == nil {
		ctx.RuleCheckResult.ResetToPass()
	} else {
		ctx.RuleCheckResult = ruleCheckRet
	}

	// execute statistic slot
	ss := sc.stats
	ruleCheckRet = ctx.RuleCheckResult
	if len(ss) > 0 {
		for _, s := range ss {
			// indicate the result of rule based checking slot.
			if !ruleCheckRet.IsBlocked() {
				s.OnEntryPassed(ctx)
			} else {
				// The block error should not be nil.
				s.OnEntryBlocked(ctx, ruleCheckRet.blockErr)
			}
		}
	}
	return ruleCheckRet
}

// slotChain退出并执行onCompleted函数
func (sc *SlotChain) exit(ctx *EntryContext) {
	if ctx == nil || ctx.Entry() == nil {
		logging.Error(errors.New("entryContext or SentinelEntry is nil"),
			"EntryContext or SentinelEntry is nil in SlotChain.exit()", "ctx", ctx)
		return
	}

	// The OnCompleted is called only when entry passed
	// 只有pass才会调用OnCompleted
	if ctx.IsBlocked() {
		return
	}

	for _, s := range sc.stats {
		s.OnCompleted(ctx)
	}
	// relieve the context here
}
